<div align="center">

<h1>excalidash-mcp</h1>

<p><em>Read and edit the Excalidraw diagrams on your self-hosted Excalidash instance from any MCP client.</em></p>

<p align="center">
  <a href="https://github.com/jomakori/excalidash-mcp/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/jomakori/excalidash-mcp/ci.yml?logo=github&logoColor=white&label=CI" alt="CI"></a>
  <a href="https://github.com/jomakori/excalidash-mcp/blob/main/LICENSE"><img src="https://img.shields.io/github/license/jomakori/excalidash-mcp?logo=opensourceinitiative&logoColor=white&label=License" alt="License"></a>
  <a href="https://github.com/jomakori/excalidash-mcp/stargazers"><img src="https://img.shields.io/github/stars/jomakori/excalidash-mcp?logo=github&logoColor=white&label=Stars" alt="Stars"></a>
  <a href="https://github.com/jomakori/excalidash-mcp/commits/main"><img src="https://img.shields.io/github/last-commit/jomakori/excalidash-mcp?logo=git&logoColor=white&label=Last%20commit" alt="Last commit"></a>
</p>

</div>

## Quick Links

[Demo](#demo) · [What is it?](#what-is-it) · [Features](#features) · [Quickstart](#quickstart) · [Documentation](#documentation) · [License](#license)

## Demo

<!-- No capture yet — add a short GIF of a client creating and editing a drawing here. -->

## What is it?

A single-file, zero-dependency MCP stdio server that puts a self-hosted Excalidash instance in reach
of an agent: list and read drawings, create and edit them, and walk or restore their version history.

## Features

- Nine tools covering the drawing lifecycle — list, read, create, update, delete, duplicate, history, restore, collections.
- Zero runtime dependencies: standard library only, and no install step, so it runs straight from a clone.
- CSRF double-submit handled for you — the token and its cookie are fetched, sent with the `Origin` header, and refreshed once if the server rejects them.
- Compact listings: the per-drawing SVG preview and the full scene are dropped from `list_drawings`.

## Quickstart

As an MCP stdio server, in a client config:

```json
{
  "mcpServers": {
    "excalidash": {
      "command": "python3",
      "args": ["/path/to/excalidash-mcp/src/excalidash_mcp/server.py"]
    }
  }
}
```

Or straight from a clone, which is how the deployment runs it:

```bash
python3 src/excalidash_mcp/server.py
```

| Variable | Default |
| --- | --- |
| `EXCALIDASH_BASE_URL` | `http://excalidash-backend.excalidash.svc.cluster.local:8000` |
| `EXCALIDASH_ORIGIN` | `https://draw.maklab.net` |

The instance is reached in-cluster and **no credentials are needed**: auth is disabled on the
Excalidash side, so the CSRF token the server fetches from `/csrf-token` is the only gate on writes.

## Documentation

- [Tool reference](docs/tools.md) — every tool, its arguments and the call behind it.

## License

MIT — see [LICENSE](LICENSE).
