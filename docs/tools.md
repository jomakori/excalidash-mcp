# Tool reference

Every tool the server exposes, the arguments it takes and the Excalidash REST call it makes.
`GET` calls send `Origin` and `Accept`; every other call additionally sends the `x-csrf-token`
header with the cookie fetched from `GET /csrf-token`.

| Tool | Arguments | Call |
| --- | --- | --- |
| `list_drawings` | `limit` (integer, optional) | `GET /drawings` |
| `get_drawing` | `id` (required) | `GET /drawings/{id}` |
| `create_drawing` | `name` (required), `elements`, `collectionId` | `POST /drawings` |
| `update_drawing` | `id` (required), `name`, `elements` | `PUT /drawings/{id}` |
| `delete_drawing` | `id` (required) | `DELETE /drawings/{id}` |
| `duplicate_drawing` | `id` (required) | `POST /drawings/{id}/duplicate` |
| `list_drawing_history` | `id` (required) | `GET /drawings/{id}/history` |
| `restore_drawing_version` | `id` (required), `snapshotId` (required) | `POST /drawings/{id}/history/{snapshotId}/restore` |
| `list_collections` | none | `GET /collections` |

`elements` accepts either a JSON array or a JSON-encoded array of Excalidraw elements.

`list_drawings` returns summaries — `id`, `name`, `collectionId`, `elementCount`, `createdAt`,
`updatedAt` — because the API returns a full SVG preview with each drawing. `get_drawing` returns the
raw drawing including its `elements`, which can be passed back to `update_drawing`.

If a write is rejected with a 403 whose body contains `CSRF`, the server fetches a fresh token once
and retries the call a single time. Any other failure comes back as a tool result with
`isError: true` and the status in the text.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `EXCALIDASH_BASE_URL` | `http://excalidash-backend.excalidash.svc.cluster.local:8000` | API the server calls |
| `EXCALIDASH_ORIGIN` | `https://draw.maklab.net` | `Origin` header sent with every request |

## Protocol

The server speaks MCP over stdio by hand and implements `initialize`, `notifications/initialized`,
`tools/list`, `tools/call` and `ping`. Notifications get no response; an unknown method returns
JSON-RPC error `-32601`.
