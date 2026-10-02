"""Unit tests for the Excalidash MCP server: JSON-RPC surface, HTTP calls and helpers."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

import contextlib
import io
import json
import unittest
import urllib.error
from typing import Any
from unittest import mock
from urllib.parse import urlparse

from excalidash_mcp import server

CSRF = {"header": "x-csrf-token", "token": "tok-1"}


def _http_error(code, body):
    return urllib.error.HTTPError(
        "http://excalidash.test/csrf", code, "error", {}, io.BytesIO(body.encode())
    )


class _Response:
    """Minimal stand-in for the object an opener returns."""

    def __init__(self, payload):
        self._body = (
            payload if isinstance(payload, bytes) else json.dumps(payload).encode()
        )

    def read(self):
        return self._body

    def __enter__(self):
        return self

    def __exit__(self, *exc_info):
        return False


class _Recorder:
    """Replays queued responses in order and records the requests that were made."""

    def __init__(self, responses=()):
        self._responses = list(responses)
        self.requests = []

    def open(self, req, timeout=None):
        self.requests.append(req)
        if not self._responses:
            raise AssertionError(
                f"unexpected request {req.get_method()} {req.full_url}"
            )
        item = self._responses.pop(0)
        if isinstance(item, Exception):
            raise item
        return _Response(item)

    @property
    def calls(self):
        return [
            (req.get_method(), urlparse(req.full_url).path) for req in self.requests
        ]

    def headers(self, index):
        return {
            key.lower(): value for key, value in self.requests[index].headers.items()
        }


@contextlib.contextmanager
def _patched(recorder):
    original = server._opener
    server._opener = recorder
    try:
        yield recorder
    finally:
        server._opener = original


class _ServerTestCase(unittest.TestCase):
    def setUp(self):
        server._csrf.update(CSRF, token="")

    def rpc(self, method: str, **params: Any) -> Any:
        return server._handle(
            {"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
        )

    def call(self, tool: str, **arguments: Any) -> Any:
        return self.rpc("tools/call", name=tool, arguments=arguments)

    def text(self, response):
        return response["result"]["content"][0]["text"]


class JsonRpcSurfaceTests(_ServerTestCase):
    def test_initialize_returns_server_info(self):
        result = self.rpc("initialize")["result"]
        self.assertEqual("excalidash", result["serverInfo"]["name"])
        self.assertEqual("2024-11-05", result["protocolVersion"])
        self.assertIn("tools", result["capabilities"])

    def test_tools_list_returns_every_tool_with_a_schema(self):
        tools = self.rpc("tools/list")["result"]["tools"]
        self.assertEqual(
            [
                "list_drawings",
                "get_drawing",
                "create_drawing",
                "update_drawing",
                "delete_drawing",
                "duplicate_drawing",
                "list_drawing_history",
                "restore_drawing_version",
                "list_collections",
            ],
            [tool["name"] for tool in tools],
        )
        for tool in tools:
            self.assertEqual("object", tool["inputSchema"]["type"])
            self.assertTrue(tool["description"])

    def test_schemas_declare_their_required_arguments(self):
        schemas = {
            tool["name"]: tool["inputSchema"]
            for tool in self.rpc("tools/list")["result"]["tools"]
        }
        self.assertEqual(["id"], schemas["get_drawing"]["required"])
        self.assertEqual(["name"], schemas["create_drawing"]["required"])
        self.assertEqual(
            ["id", "snapshotId"], schemas["restore_drawing_version"]["required"]
        )
        self.assertNotIn("required", schemas["list_collections"])

    def test_unknown_method_returns_method_not_found(self):
        response = self.rpc("does/not/exist")
        self.assertNotIn("result", response)
        self.assertEqual(-32601, response["error"]["code"])

    def test_notification_gets_no_response(self):
        self.assertIsNone(
            server._handle({"jsonrpc": "2.0", "method": "notifications/initialized"})
        )

    def test_ping_returns_an_empty_result(self):
        self.assertEqual({}, self.rpc("ping")["result"])

    def test_main_writes_one_line_per_request(self):
        stream = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/list"}) + "\n"
        stream += (
            json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}) + "\n"
        )
        with mock.patch("sys.stdin", io.StringIO(stream)):
            out = io.StringIO()
            with contextlib.redirect_stdout(out):
                server.main()
        lines = out.getvalue().splitlines()
        self.assertEqual(1, len(lines))
        self.assertEqual(9, len(json.loads(lines[0])["result"]["tools"]))


class ToolCallTests(_ServerTestCase):
    def test_list_drawings_summarises_and_applies_limit(self):
        recorder = _Recorder(
            [
                {
                    "drawings": [
                        {
                            "id": "a",
                            "name": "A",
                            "collectionId": None,
                            "elements": [{"x": 1}, {"x": 2}],
                            "preview": "<svg/>",
                        },
                        {"id": "b", "name": "B", "elements": [], "preview": "<svg/>"},
                    ]
                }
            ]
        )
        with _patched(recorder):
            payload = json.loads(self.text(self.call("list_drawings", limit=1)))
        self.assertEqual([("GET", "/drawings")], recorder.calls)
        self.assertEqual(1, payload["count"])
        self.assertEqual("a", payload["drawings"][0]["id"])
        self.assertEqual(2, payload["drawings"][0]["elementCount"])
        self.assertNotIn("preview", payload["drawings"][0])

    def test_get_drawing_returns_the_raw_scene(self):
        recorder = _Recorder(
            [{"id": "d1", "name": "D", "elements": [{"type": "rectangle"}]}]
        )
        with _patched(recorder):
            payload = json.loads(self.text(self.call("get_drawing", id="d1")))
        self.assertEqual([("GET", "/drawings/d1")], recorder.calls)
        self.assertEqual([{"type": "rectangle"}], payload["elements"])

    def test_create_drawing_posts_with_csrf_and_origin_headers(self):
        recorder = _Recorder([CSRF, {"id": "new", "name": "New", "elements": []}])
        with _patched(recorder):
            response = self.call(
                "create_drawing", name="New", elements='[{"type":"rectangle"}]'
            )
        self.assertEqual(
            [("GET", "/csrf-token"), ("POST", "/drawings")], recorder.calls
        )
        headers = recorder.headers(1)
        self.assertEqual("tok-1", headers["x-csrf-token"])
        self.assertEqual(server.ORIGIN, headers["origin"])
        self.assertEqual("application/json", headers["content-type"])
        self.assertEqual(
            {"name": "New", "elements": [{"type": "rectangle"}]},
            json.loads(recorder.requests[1].data),
        )
        self.assertFalse(response["result"]["isError"])

    def test_update_drawing_puts_the_named_fields(self):
        recorder = _Recorder([CSRF, {"id": "d1", "name": "Renamed", "elements": []}])
        with _patched(recorder):
            response = self.call("update_drawing", id="d1", name="Renamed", elements=[])
        self.assertEqual(
            [("GET", "/csrf-token"), ("PUT", "/drawings/d1")], recorder.calls
        )
        self.assertEqual("tok-1", recorder.headers(1)["x-csrf-token"])
        self.assertEqual(
            {"name": "Renamed", "elements": []}, json.loads(recorder.requests[1].data)
        )
        self.assertFalse(response["result"]["isError"])

    def test_update_drawing_without_changes_is_an_error_result(self):
        recorder = _Recorder()
        with _patched(recorder):
            response = self.call("update_drawing", id="d1")
        self.assertEqual([], recorder.calls)
        self.assertTrue(response["result"]["isError"])

    def test_remaining_mutating_tools_use_the_expected_verb_and_path(self):
        cases = [
            ("delete_drawing", {"id": "d1"}, ("DELETE", "/drawings/d1"), None),
            (
                "duplicate_drawing",
                {"id": "d1"},
                ("POST", "/drawings/d1/duplicate"),
                None,
            ),
            (
                "restore_drawing_version",
                {"id": "d1", "snapshotId": "s1"},
                ("POST", "/drawings/d1/history/s1/restore"),
                {},
            ),
        ]
        for name, arguments, expected, body in cases:
            with self.subTest(tool=name):
                recorder = _Recorder([CSRF, {"id": "d1"}])
                with _patched(recorder):
                    response = self.call(name, **arguments)
                self.assertEqual([("GET", "/csrf-token"), expected], recorder.calls)
                self.assertEqual("tok-1", recorder.headers(1)["x-csrf-token"])
                data = recorder.requests[1].data
                self.assertEqual(body, json.loads(data) if data else None)
                self.assertFalse(response["result"]["isError"])

    def test_read_only_tools_skip_the_csrf_fetch(self):
        cases = [
            ("list_drawing_history", {"id": "d1"}, ("GET", "/drawings/d1/history")),
            ("list_collections", {}, ("GET", "/collections")),
        ]
        for name, arguments, expected in cases:
            with self.subTest(tool=name):
                recorder = _Recorder([{"versions": []}])
                with _patched(recorder):
                    response = self.call(name, **arguments)
                self.assertEqual([expected], recorder.calls)
                self.assertFalse(response["result"]["isError"])

    def test_unknown_tool_name_is_an_error_result(self):
        recorder = _Recorder()
        with _patched(recorder):
            response = self.call("no_such_tool")
        self.assertEqual([], recorder.calls)
        self.assertIn("result", response)
        self.assertTrue(response["result"]["isError"])
        self.assertIn("unknown tool no_such_tool", self.text(response))

    def test_missing_required_argument_is_an_error_result(self):
        recorder = _Recorder()
        with _patched(recorder):
            response = self.call("get_drawing")
        self.assertEqual([], recorder.calls)
        self.assertTrue(response["result"]["isError"])

    def test_non_array_elements_is_an_error_result(self):
        recorder = _Recorder()
        with _patched(recorder):
            response = self.call("create_drawing", name="New", elements=5)
        self.assertEqual([], recorder.calls)
        self.assertTrue(response["result"]["isError"])
        self.assertIn("JSON array", self.text(response))


class CsrfRetryTests(_ServerTestCase):
    def test_rejected_token_is_refreshed_once_then_retried(self):
        recorder = _Recorder(
            [
                CSRF,
                _http_error(403, '{"detail":"CSRF token missing"}'),
                {"header": "x-csrf-token", "token": "tok-2"},
                {"id": "d1", "name": "Renamed", "elements": []},
            ]
        )
        with _patched(recorder):
            response = self.call("update_drawing", id="d1", name="Renamed")
        self.assertEqual(
            [
                ("GET", "/csrf-token"),
                ("PUT", "/drawings/d1"),
                ("GET", "/csrf-token"),
                ("PUT", "/drawings/d1"),
            ],
            recorder.calls,
        )
        self.assertEqual("tok-2", recorder.headers(3)["x-csrf-token"])
        self.assertFalse(response["result"]["isError"])

    def test_a_second_rejection_is_not_retried_again(self):
        recorder = _Recorder(
            [
                CSRF,
                _http_error(403, "CSRF"),
                {"header": "x-csrf-token", "token": "tok-2"},
                _http_error(403, "CSRF"),
            ]
        )
        with _patched(recorder):
            response = self.call("update_drawing", id="d1", name="Renamed")
        self.assertEqual(4, len(recorder.calls))
        self.assertTrue(response["result"]["isError"])
        self.assertIn("403", self.text(response))

    def test_non_csrf_forbidden_becomes_an_error_result(self):
        recorder = _Recorder([CSRF, _http_error(403, '{"detail":"not allowed"}')])
        with _patched(recorder):
            response = self.call("delete_drawing", id="d1")
        self.assertEqual(
            [("GET", "/csrf-token"), ("DELETE", "/drawings/d1")], recorder.calls
        )
        self.assertTrue(response["result"]["isError"])
        self.assertIn("403", self.text(response))

    def test_server_error_becomes_an_error_result(self):
        recorder = _Recorder([CSRF, _http_error(500, "boom")])
        with _patched(recorder):
            response = self.call("delete_drawing", id="d1")
        self.assertEqual(2, len(recorder.calls))
        self.assertTrue(response["result"]["isError"])
        self.assertIn("500", self.text(response))


class HelperTests(unittest.TestCase):
    def test_summarise_drops_preview_and_elements(self):
        summary = server._summarise(
            {
                "id": "a",
                "name": "A",
                "collectionId": "c",
                "elements": [{"x": 1}],
                "preview": "<svg/>",
                "createdAt": "t1",
                "updatedAt": "t2",
            }
        )
        self.assertEqual(
            {
                "id": "a",
                "name": "A",
                "collectionId": "c",
                "elementCount": 1,
                "createdAt": "t1",
                "updatedAt": "t2",
            },
            summary,
        )

    def test_summarise_copes_with_a_missing_scene(self):
        self.assertEqual(0, server._summarise({"id": "a"})["elementCount"])

    def test_elements_accepts_a_json_string_or_a_list(self):
        expected = [{"type": "rectangle"}]
        self.assertEqual(expected, server._elements('[{"type": "rectangle"}]'))
        self.assertEqual(expected, server._elements([{"type": "rectangle"}]))

    def test_elements_treats_empty_input_as_an_empty_scene(self):
        self.assertEqual([], server._elements(None))
        self.assertEqual([], server._elements(""))
        self.assertEqual([], server._elements([]))

    def test_elements_rejects_a_non_array(self):
        for value in ('{"type": "rectangle"}', 5, "5"):
            with self.subTest(value=value), self.assertRaises(TypeError):
                server._elements(value)


if __name__ == "__main__":
    unittest.main()
