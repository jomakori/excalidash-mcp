package mcpserver_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jomakori/excalidash-mcp/internal/excalidash"
	"github.com/jomakori/excalidash-mcp/internal/mcpserver"
)

var wantTools = []string{
	"create_drawing",
	"delete_drawing",
	"duplicate_drawing",
	"get_drawing",
	"list_collections",
	"list_drawing_history",
	"list_drawings",
	"restore_drawing_version",
	"update_drawing",
}

// connect starts the MCP server and a client over in-memory transports.
func connect(t *testing.T, backendURL string) *mcp.ClientSession {
	t.Helper()

	api, err := excalidash.New(backendURL, excalidash.DefaultOrigin)
	if err != nil {
		t.Fatalf("New client: %v", err)
	}

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := mcpserver.New("test", api).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { clientSession.Close() })

	return clientSession
}

// schemaOf returns the advertised input schema of a tool.
func schemaOf(t *testing.T, tools []*mcp.Tool, name string) (map[string]any, []string) {
	t.Helper()

	for _, tool := range tools {
		if tool.Name != name {
			continue
		}
		encoded, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s schema: %v", name, err)
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatalf("decode %s schema: %v", name, err)
		}
		return schema.Properties, schema.Required
	}
	t.Fatalf("tool %s is not advertised", name)
	return nil, nil
}

func TestServerAdvertisesNineTools(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"drawings":[]}`)
	}))
	defer backend.Close()

	listed, err := connect(t, backend.URL).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	got := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		got = append(got, tool.Name)
	}
	sort.Strings(got)
	if !slices.Equal(got, wantTools) {
		t.Fatalf("tools = %v, want %v", got, wantTools)
	}

	for _, tool := range listed.Tools {
		if tool.Description == "" {
			t.Errorf("tool %s has no description", tool.Name)
		}
	}
}

func TestToolParametersMatchTheAPIContract(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"drawings":[]}`)
	}))
	defer backend.Close()

	listed, err := connect(t, backend.URL).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for tool, want := range map[string][]string{
		"list_drawings":           {"limit"},
		"get_drawing":             {"id"},
		"create_drawing":          {"collectionId", "elements", "name"},
		"update_drawing":          {"elements", "id", "name"},
		"delete_drawing":          {"id"},
		"duplicate_drawing":       {"id"},
		"list_drawing_history":    {"id"},
		"restore_drawing_version": {"id", "snapshotId"},
		"list_collections":        {},
	} {
		properties, _ := schemaOf(t, listed.Tools, tool)
		got := make([]string, 0, len(properties))
		for property := range properties {
			got = append(got, property)
		}
		sort.Strings(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s parameters = %v, want %v", tool, got, want)
		}
	}

	if _, required := schemaOf(t, listed.Tools, "restore_drawing_version"); !slices.Contains(required, "snapshotId") {
		t.Errorf("restore_drawing_version required = %v, want it to contain snapshotId", required)
	}
}

func TestListDrawingsReturnsSummaries(t *testing.T) {
	var (
		mu         sync.Mutex
		originSeen string
	)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		originSeen = r.Header.Get("Origin")
		mu.Unlock()
		io.WriteString(w, `{"drawings":[{"id":"d1","name":"alpha","collectionId":"c1","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z","preview":"<svg/>","elements":[{"type":"rectangle"}]}]}`)
	}))
	defer backend.Close()

	result, err := connect(t, backend.URL).CallTool(context.Background(), &mcp.CallToolParams{Name: "list_drawings"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("list_drawings returned an error result: %v", result.Content)
	}
	if len(result.Content) == 0 {
		t.Fatal("list_drawings returned no content")
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var payload struct {
		Count    int `json:"count"`
		Drawings []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			ElementCount int    `json:"elementCount"`
		} `json:"drawings"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("decode result %q: %v", text, err)
	}
	if payload.Count != 1 || len(payload.Drawings) != 1 {
		t.Fatalf("result = %s, want one drawing", text)
	}
	if payload.Drawings[0].ID != "d1" || payload.Drawings[0].Name != "alpha" || payload.Drawings[0].ElementCount != 1 {
		t.Errorf("summary = %+v", payload.Drawings[0])
	}
	if strings.Contains(text, "preview") || strings.Contains(text, "elements") {
		t.Errorf("result %s leaks the preview or the scene", text)
	}

	mu.Lock()
	defer mu.Unlock()
	if originSeen != excalidash.DefaultOrigin {
		t.Errorf("Origin = %q, want %q", originSeen, excalidash.DefaultOrigin)
	}
}

func TestAPIFailureBecomesAnErrorResult(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "boom: the backend exploded")
	}))
	defer backend.Close()

	result, err := connect(t, backend.URL).CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_drawing",
		Arguments: map[string]any{"id": "d1"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatal("get_drawing did not report an error result")
	}

	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "500") || !strings.Contains(text, "boom: the backend exploded") {
		t.Errorf("error result = %q, want the status and the backend message", text)
	}
}
