// Package mcpserver exposes the Excalidash REST API as MCP tools.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jomakori/excalidash-mcp/internal/excalidash"
)

const (
	serverName   = "excalidash"
	instructions = "Read and edit Excalidraw diagrams on a self-hosted Excalidash instance. " +
		"get_drawing returns the scene elements; pass them back to update_drawing to change a diagram."
)

// New registers every Excalidash tool on a new MCP server.
func New(version string, api *excalidash.Client) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: serverName, Version: version},
		&mcp.ServerOptions{Instructions: instructions},
	)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_drawings",
		Description: "List drawings, newest first. Returns id, name, collection and element count per drawing.",
	}, newListDrawingsHandler(api))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_drawing",
		Description: "Fetch one drawing by id, including its Excalidraw elements.",
	}, newGetDrawingHandler(api))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_drawing",
		Description: "Create a drawing and return its id, name, and element count.",
	}, newCreateDrawingHandler(api))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_drawing",
		Description: "Rename a drawing and/or replace its elements. Provide at least one of name or elements.",
	}, newUpdateDrawingHandler(api))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_drawing",
		Description: "Delete a drawing by id.",
	}, newDeleteDrawingHandler(api))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "duplicate_drawing",
		Description: "Copy a drawing, returning the new drawing's id.",
	}, newDuplicateDrawingHandler(api))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_drawing_history",
		Description: "List the saved versions of a drawing.",
	}, newListDrawingHistoryHandler(api))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "restore_drawing_version",
		Description: "Restore a drawing to a version from its history.",
	}, newRestoreDrawingVersionHandler(api))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_collections",
		Description: "List collections used to group drawings.",
	}, newListCollectionsHandler(api))

	return server
}

type listDrawingsArgs struct {
	Limit int `json:"limit,omitempty" jsonschema:"Maximum drawings to return."`
}

type drawingIDArgs struct {
	ID string `json:"id" jsonschema:"Drawing id."`
}

type createDrawingArgs struct {
	Name         string `json:"name" jsonschema:"Drawing name."`
	Elements     any    `json:"elements,omitempty" jsonschema:"JSON array of Excalidraw elements."`
	CollectionID string `json:"collectionId,omitempty" jsonschema:"Optional collection id."`
}

type updateDrawingArgs struct {
	ID       string  `json:"id" jsonschema:"Drawing id."`
	Name     *string `json:"name,omitempty" jsonschema:"New name."`
	Elements any     `json:"elements,omitempty" jsonschema:"Replacement JSON array of Excalidraw elements."`
}

type restoreDrawingVersionArgs struct {
	ID         string `json:"id" jsonschema:"Drawing id."`
	SnapshotID string `json:"snapshotId" jsonschema:"Version id to restore."`
}

func newListDrawingsHandler(api *excalidash.Client) mcp.ToolHandlerFor[listDrawingsArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args listDrawingsArgs) (*mcp.CallToolResult, any, error) {
		body, err := api.Get(ctx, "/drawings")
		if err != nil {
			return errorResult(err)
		}

		drawings := summariseAll(body)
		if args.Limit > 0 && args.Limit < len(drawings) {
			drawings = drawings[:args.Limit]
		}
		return jsonResult(map[string]any{"count": len(drawings), "drawings": drawings})
	}
}

func newGetDrawingHandler(api *excalidash.Client) mcp.ToolHandlerFor[drawingIDArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args drawingIDArgs) (*mcp.CallToolResult, any, error) {
		body, err := api.Get(ctx, drawingPath(args.ID))
		if err != nil {
			return errorResult(err)
		}
		return jsonResult(body)
	}
}

func newCreateDrawingHandler(api *excalidash.Client) mcp.ToolHandlerFor[createDrawingArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args createDrawingArgs) (*mcp.CallToolResult, any, error) {
		elements, err := excalidash.Elements(args.Elements)
		if err != nil {
			return errorResult(err)
		}

		payload := map[string]any{"name": args.Name, "elements": elements}
		if args.CollectionID != "" {
			payload["collectionId"] = args.CollectionID
		}

		body, err := api.Post(ctx, "/drawings", payload)
		if err != nil {
			return errorResult(err)
		}
		return jsonResult(summariseOne(body))
	}
}

func newUpdateDrawingHandler(api *excalidash.Client) mcp.ToolHandlerFor[updateDrawingArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args updateDrawingArgs) (*mcp.CallToolResult, any, error) {
		payload := map[string]any{}
		if args.Name != nil {
			payload["name"] = *args.Name
		}
		if args.Elements != nil {
			elements, err := excalidash.Elements(args.Elements)
			if err != nil {
				return errorResult(err)
			}
			payload["elements"] = elements
		}
		if len(payload) == 0 {
			return errorResult(errors.New("update_drawing needs name and/or elements"))
		}

		body, err := api.Put(ctx, drawingPath(args.ID), payload)
		if err != nil {
			return errorResult(err)
		}
		return jsonResult(summariseOne(body))
	}
}

func newDeleteDrawingHandler(api *excalidash.Client) mcp.ToolHandlerFor[drawingIDArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args drawingIDArgs) (*mcp.CallToolResult, any, error) {
		body, err := api.Delete(ctx, drawingPath(args.ID))
		if err != nil {
			return errorResult(err)
		}
		return jsonResult(body)
	}
}

func newDuplicateDrawingHandler(api *excalidash.Client) mcp.ToolHandlerFor[drawingIDArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args drawingIDArgs) (*mcp.CallToolResult, any, error) {
		body, err := api.Post(ctx, drawingPath(args.ID)+"/duplicate", nil)
		if err != nil {
			return errorResult(err)
		}
		return jsonResult(summariseOne(body))
	}
}

func newListDrawingHistoryHandler(api *excalidash.Client) mcp.ToolHandlerFor[drawingIDArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args drawingIDArgs) (*mcp.CallToolResult, any, error) {
		body, err := api.Get(ctx, drawingPath(args.ID)+"/history")
		if err != nil {
			return errorResult(err)
		}
		return jsonResult(body)
	}
}

func newRestoreDrawingVersionHandler(api *excalidash.Client) mcp.ToolHandlerFor[restoreDrawingVersionArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args restoreDrawingVersionArgs) (*mcp.CallToolResult, any, error) {
		body, err := api.Post(ctx, drawingPath(args.ID)+"/restore", map[string]any{"snapshotId": args.SnapshotID})
		if err != nil {
			return errorResult(err)
		}
		return jsonResult(body)
	}
}

func newListCollectionsHandler(api *excalidash.Client) mcp.ToolHandlerFor[struct{}, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		body, err := api.Get(ctx, "/collections")
		if err != nil {
			return errorResult(err)
		}
		return jsonResult(body)
	}
}

func drawingPath(id string) string {
	return "/drawings/" + url.PathEscape(id)
}

func summariseAll(body any) []excalidash.Summary {
	object, _ := body.(map[string]any)
	raw, _ := object["drawings"].([]any)

	drawings := make([]excalidash.Summary, 0, len(raw))
	for _, item := range raw {
		if drawing, ok := item.(map[string]any); ok {
			drawings = append(drawings, excalidash.Summarise(drawing))
		}
	}
	return drawings
}

func summariseOne(body any) any {
	drawing, ok := body.(map[string]any)
	if !ok {
		return body
	}
	return excalidash.Summarise(drawing)
}

func jsonResult(value any) (*mcp.CallToolResult, any, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("encode tool result: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}}, nil, nil
}

func errorResult(err error) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}, nil, nil
}
