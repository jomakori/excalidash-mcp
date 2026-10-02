<div align="center">

<h1>excalidash-mcp</h1>

<p><em>Read and edit Excalidraw diagrams on a self-hosted Excalidash instance from any MCP client.</em></p>

<p>
  <a href="https://github.com/jomakori/excalidash-mcp/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/jomakori/excalidash-mcp/ci.yml?logo=githubactions&logoColor=white&label=CI" alt="CI"></a>
  <a href="https://github.com/jomakori/excalidash-mcp/blob/main/LICENSE"><img src="https://img.shields.io/github/license/jomakori/excalidash-mcp?logo=opensourceinitiative&logoColor=white&label=License" alt="License"></a>
  <a href="https://github.com/jomakori/excalidash-mcp/stargazers"><img src="https://img.shields.io/github/stars/jomakori/excalidash-mcp?logo=github&logoColor=white&label=Stars" alt="Stars"></a>
  <a href="https://github.com/jomakori/excalidash-mcp/commits/main"><img src="https://img.shields.io/github/last-commit/jomakori/excalidash-mcp?logo=git&logoColor=white&label=Last%20commit" alt="Last commit"></a>
</p>

</div>

## Quick Links

- [Demo](#demo)
- [What is it?](#what-is-it)
- [Features](#features)
- [Quickstart](#quickstart)
- [License](#license)

## Demo

<!-- TODO: replace this placeholder with a capture of the server answering tools/list in an MCP client. -->

No capture yet.

## What is it?

A single static binary that speaks MCP over stdio and forwards every call to the Excalidash REST API. Point your MCP client at it and the client gains nine tools for listing, reading, creating, editing, duplicating and versioning drawings, plus one for the collections they live in.

## Features

- Nine tools covering drawings and collections, including version history and restore.
- Origin and CSRF handled for you, so writes satisfy the backend's browser checks.
- Listing and write results are compact summaries: id, name, collection, element count, timestamps.
- No credentials to store; point it at a base URL and an origin.

## Quickstart

Add the server to your MCP client configuration:

```json
{
  "mcpServers": {
    "excalidash": {
      "command": "/usr/local/bin/excalidash-mcp",
      "env": {
        "EXCALIDASH_BASE_URL": "http://excalidash-backend.excalidash.svc.cluster.local:8000",
        "EXCALIDASH_ORIGIN": "https://draw.maklab.net"
      }
    }
  }
}
```

Build it yourself with Go 1.25 or newer:

```bash
git clone https://github.com/jomakori/excalidash-mcp
cd excalidash-mcp
CGO_ENABLED=0 go build -o excalidash-mcp .
```

Or take a pinned release build — this is the arm64 asset; use `excalidash-mcp-linux-amd64.tar.gz` on x86-64:

```bash
curl -fsSL -o excalidash-mcp-linux-arm64.tar.gz \
  https://github.com/jomakori/excalidash-mcp/releases/download/v1.0.0/excalidash-mcp-linux-arm64.tar.gz
tar -xzf excalidash-mcp-linux-arm64.tar.gz
```

## License

MIT. See [LICENSE](LICENSE).
