// Command excalidash-mcp serves the Excalidash REST API over stdio MCP.
package main

import (
	"context"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jomakori/excalidash-mcp/internal/excalidash"
	"github.com/jomakori/excalidash-mcp/internal/mcpserver"
)

// version is stamped by the release build through -ldflags.
var version = "dev"

func main() {
	api, err := excalidash.New(envOr("EXCALIDASH_BASE_URL", excalidash.DefaultBaseURL), envOr("EXCALIDASH_ORIGIN", excalidash.DefaultOrigin))
	if err != nil {
		log.Fatalf("excalidash-mcp: %v", err)
	}

	server := mcpserver.New(version, api)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("excalidash-mcp: %v", err)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
