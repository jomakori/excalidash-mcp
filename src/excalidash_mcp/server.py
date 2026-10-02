#!/usr/bin/env python3
"""MCP server exposing the in-cluster Excalidash REST API as diagram tools."""

import json
import os
import sys
import urllib.error
import urllib.request
from http.cookiejar import CookieJar
from typing import Any

BASE_URL = os.environ.get(
    "EXCALIDASH_BASE_URL",
    "http://excalidash-backend.excalidash.svc.cluster.local:8000",
).rstrip("/")
ORIGIN = os.environ.get("EXCALIDASH_ORIGIN", "https://draw.maklab.net")
SERVER_NAME = "excalidash"
SERVER_VERSION = "1.0.0"

_jar = CookieJar()
_opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(_jar))
_csrf = {"header": "x-csrf-token", "token": ""}


class ApiError(Exception):
    """Raised for a non-2xx Excalidash response."""


def _csrf_token() -> dict:
    """Fetch a CSRF token, keeping the cookie it is bound to."""
    req = urllib.request.Request(
        BASE_URL + "/csrf-token", headers={"Origin": ORIGIN, "Accept": "application/json"}
    )
    with _opener.open(req, timeout=30) as resp:
        body = json.loads(resp.read().decode())
    _csrf["header"] = body.get("header") or _csrf["header"]
    _csrf["token"] = body.get("token", "")
    return _csrf


def _request(method: str, path: str, payload=None) -> Any:
    """Call the API, refreshing the CSRF token once if it is rejected."""
    if method != "GET":
        _csrf_token()
    for attempt in (1, 2):
        data = json.dumps(payload).encode() if payload is not None else None
        headers = {"Origin": ORIGIN, "Accept": "application/json"}
        if data is not None:
            headers["Content-Type"] = "application/json"
        if method != "GET":
            headers[_csrf["header"]] = _csrf["token"]
        req = urllib.request.Request(
            BASE_URL + path, data=data, headers=headers, method=method
        )
        try:
            with _opener.open(req, timeout=60) as resp:
                raw = resp.read().decode()
                return json.loads(raw) if raw else {}
        except urllib.error.HTTPError as err:
            raw = err.read().decode(errors="replace")
            if err.code == 403 and "CSRF" in raw and attempt == 1:
                _csrf["token"] = ""
                continue
            raise ApiError(f"HTTP {err.code} {method} {path}: {raw[:400]}") from err
    raise ApiError(f"CSRF token could not be refreshed for {method} {path}")


def _summarise(drawing: dict) -> dict:
    """Drop the scene and preview so a listing stays small."""
    return {
        "id": drawing.get("id"),
        "name": drawing.get("name"),
        "collectionId": drawing.get("collectionId"),
        "elementCount": len(drawing.get("elements") or []),
        "createdAt": drawing.get("createdAt"),
        "updatedAt": drawing.get("updatedAt"),
    }


def _elements(value) -> list:
    """Accept a scene as a JSON string or an already-decoded list."""
    if value in (None, "", []):
        return []
    if isinstance(value, str):
        value = json.loads(value)
    if not isinstance(value, list):
        raise ValueError("elements must be a JSON array or a JSON-encoded array")
    return value


TOOLS = [
    {
        "name": "list_drawings",
        "description": "List drawings, newest first. Returns id, name, collection and element count per drawing.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "limit": {"type": "integer", "description": "Maximum drawings to return."}
            },
        },
    },
    {
        "name": "get_drawing",
        "description": "Read one drawing's full Excalidraw scene: name plus the elements array.",
        "inputSchema": {
            "type": "object",
            "properties": {"id": {"type": "string", "description": "Drawing id."}},
            "required": ["id"],
        },
    },
    {
        "name": "create_drawing",
        "description": "Create a drawing from Excalidraw elements.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "name": {"type": "string", "description": "Drawing name."},
                "elements": {
                    "type": "string",
                    "description": "JSON array of Excalidraw elements.",
                },
                "collectionId": {"type": "string", "description": "Optional collection id."},
            },
            "required": ["name"],
        },
    },
    {
        "name": "update_drawing",
        "description": "Replace a drawing's name and/or its full elements array.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "id": {"type": "string", "description": "Drawing id."},
                "name": {"type": "string", "description": "New name."},
                "elements": {
                    "type": "string",
                    "description": "Replacement JSON array of Excalidraw elements.",
                },
            },
            "required": ["id"],
        },
    },
    {
        "name": "delete_drawing",
        "description": "Delete a drawing permanently.",
        "inputSchema": {
            "type": "object",
            "properties": {"id": {"type": "string", "description": "Drawing id."}},
            "required": ["id"],
        },
    },
    {
        "name": "duplicate_drawing",
        "description": "Copy a drawing, returning the new drawing's id.",
        "inputSchema": {
            "type": "object",
            "properties": {"id": {"type": "string", "description": "Drawing id."}},
            "required": ["id"],
        },
    },
    {
        "name": "list_drawing_history",
        "description": "List saved versions of a drawing.",
        "inputSchema": {
            "type": "object",
            "properties": {"id": {"type": "string", "description": "Drawing id."}},
            "required": ["id"],
        },
    },
    {
        "name": "restore_drawing_version",
        "description": "Restore a drawing to a previously saved version.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "id": {"type": "string", "description": "Drawing id."},
                "snapshotId": {"type": "string", "description": "Version id to restore."},
            },
            "required": ["id", "snapshotId"],
        },
    },
    {
        "name": "list_collections",
        "description": "List drawing collections.",
        "inputSchema": {"type": "object", "properties": {}},
    },
]


def _call(name: str, args: dict) -> Any:
    """Dispatch one tool call to the API."""
    if name == "list_drawings":
        body = _request("GET", "/drawings")
        drawings = [_summarise(d) for d in body.get("drawings", [])]
        limit = args.get("limit")
        if isinstance(limit, int) and limit > 0:
            drawings = drawings[:limit]
        return {"count": len(drawings), "drawings": drawings}

    if name == "get_drawing":
        return _request("GET", f"/drawings/{args['id']}")

    if name == "create_drawing":
        payload = {"name": args["name"], "elements": _elements(args.get("elements"))}
        if args.get("collectionId"):
            payload["collectionId"] = args["collectionId"]
        return _summarise(_request("POST", "/drawings", payload))

    if name == "update_drawing":
        payload = {}
        if "name" in args:
            payload["name"] = args["name"]
        if "elements" in args:
            payload["elements"] = _elements(args.get("elements"))
        if not payload:
            raise ValueError("update_drawing needs name and/or elements")
        return _summarise(_request("PUT", f"/drawings/{args['id']}", payload))

    if name == "delete_drawing":
        return _request("DELETE", f"/drawings/{args['id']}")

    if name == "duplicate_drawing":
        return _summarise(_request("POST", f"/drawings/{args['id']}/duplicate"))

    if name == "list_drawing_history":
        return _request("GET", f"/drawings/{args['id']}/history")

    if name == "restore_drawing_version":
        return _request(
            "POST",
            f"/drawings/{args['id']}/history/{args['snapshotId']}/restore",
            {},
        )

    if name == "list_collections":
        return _request("GET", "/collections")

    raise ValueError(f"unknown tool {name}")


def _text(payload: Any, is_error: bool = False) -> dict:
    """Wrap a result as an MCP tool response."""
    body = payload if isinstance(payload, str) else json.dumps(payload, indent=2)
    return {"content": [{"type": "text", "text": body}], "isError": is_error}


def _handle(msg: dict) -> dict | None:
    """Handle one JSON-RPC message; None means no response is owed."""
    method = msg.get("method")
    msg_id = msg.get("id")

    if method == "initialize":
        return {
            "jsonrpc": "2.0",
            "id": msg_id,
            "result": {
                "protocolVersion": "2024-11-05",
                "capabilities": {"tools": {"listChanged": False}},
                "serverInfo": {"name": SERVER_NAME, "version": SERVER_VERSION},
                "instructions": (
                    "Read and edit Excalidraw diagrams in the self-hosted Excalidash "
                    "instance. get_drawing returns the scene elements; pass them back "
                    "to update_drawing to change a diagram."
                ),
            },
        }
    if method in ("notifications/initialized", "notifications/cancelled"):
        return None
    if method == "tools/list":
        return {"jsonrpc": "2.0", "id": msg_id, "result": {"tools": TOOLS}}
    if method == "ping":
        return {"jsonrpc": "2.0", "id": msg_id, "result": {}}
    if method == "tools/call":
        params = msg.get("params") or {}
        try:
            return {
                "jsonrpc": "2.0",
                "id": msg_id,
                "result": _text(_call(params.get("name", ""), params.get("arguments") or {})),
            }
        except (ApiError, ValueError, KeyError, json.JSONDecodeError) as err:
            return {"jsonrpc": "2.0", "id": msg_id, "result": _text(str(err), True)}
    return {
        "jsonrpc": "2.0",
        "id": msg_id,
        "error": {"code": -32601, "message": f"method not found: {method}"},
    }


def main() -> None:
    """Serve MCP over stdio."""
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            msg = json.loads(line)
        except json.JSONDecodeError:
            continue
        response = _handle(msg)
        if response is None:
            continue
        sys.stdout.write(json.dumps(response) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    main()
