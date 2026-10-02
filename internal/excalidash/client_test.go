package excalidash_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jomakori/excalidash-mcp/internal/excalidash"
)

const testOrigin = "https://draw.example.test"

// spy records the CSRF and write requests a test backend saw.
type spy struct {
	mu         sync.Mutex
	csrfCalls  int
	writeCalls int
	header     string
	token      string
	cookie     string
	origin     string
}

func (s *spy) noteCSRF() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.csrfCalls++
}

func (s *spy) noteWrite(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeCalls++
	s.header = r.Header.Get("x-server-csrf")
	s.token = r.Header.Get("x-csrf-token")
	s.origin = r.Header.Get("Origin")
	if cookie, err := r.Cookie("csrf"); err == nil {
		s.cookie = cookie.Value
	}
}

func (s *spy) counts() (csrf, writes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.csrfCalls, s.writeCalls
}

func (s *spy) writeHeaders() (header, token, cookie, origin string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.header, s.token, s.cookie, s.origin
}

func newClient(t *testing.T, baseURL string) *excalidash.Client {
	t.Helper()
	client, err := excalidash.New(baseURL, testOrigin)
	if err != nil {
		t.Fatalf("New(%q): %v", baseURL, err)
	}
	return client
}

func TestWriteCarriesCookieCSRFHeaderAndOrigin(t *testing.T) {
	var observed spy

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/csrf-token":
			observed.noteCSRF()
			http.SetCookie(w, &http.Cookie{Name: "csrf", Value: "cookie-value", Path: "/"})
			fmt.Fprint(w, `{"token":"token-value","header":"x-server-csrf"}`)
		case "/drawings":
			observed.noteWrite(r)
			fmt.Fprint(w, `{"id":"d1","name":"flow","elements":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()

	if _, err := newClient(t, backend.URL).Post(context.Background(), "/drawings", map[string]any{"name": "flow"}); err != nil {
		t.Fatalf("Post: %v", err)
	}

	header, token, cookie, origin := observed.writeHeaders()
	if header != "token-value" {
		t.Errorf("header named by the server = %q, want %q", header, "token-value")
	}
	if token != "" {
		t.Errorf("fallback header x-csrf-token = %q, want it unset", token)
	}
	if cookie != "cookie-value" {
		t.Errorf("cookie = %q, want %q", cookie, "cookie-value")
	}
	if origin != testOrigin {
		t.Errorf("Origin = %q, want %q", origin, testOrigin)
	}
	if csrf, writes := observed.counts(); csrf != 1 || writes != 1 {
		t.Errorf("csrf-token fetches = %d, writes = %d, want 1 and 1", csrf, writes)
	}
}

func TestReadsSkipCSRFToken(t *testing.T) {
	var observed spy

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/csrf-token" {
			observed.noteCSRF()
		}
		fmt.Fprint(w, `{"drawings":[]}`)
	}))
	defer backend.Close()

	if _, err := newClient(t, backend.URL).Get(context.Background(), "/drawings"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if csrf, _ := observed.counts(); csrf != 0 {
		t.Errorf("csrf-token fetches = %d, want 0", csrf)
	}
}

func TestStaleCSRFRefreshesOnceAndRetriesOnce(t *testing.T) {
	var (
		mu        sync.Mutex
		csrfCalls int
		writeHits int
	)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/csrf-token" {
			mu.Lock()
			csrfCalls++
			token := fmt.Sprintf("token-%d", csrfCalls)
			mu.Unlock()
			http.SetCookie(w, &http.Cookie{Name: "csrf", Value: token, Path: "/"})
			fmt.Fprintf(w, `{"token":%q,"header":"x-csrf-token"}`, token)
			return
		}

		mu.Lock()
		writeHits++
		attempt := writeHits
		mu.Unlock()

		if attempt == 1 {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"detail":"CSRF token missing or invalid"}`)
			return
		}
		fmt.Fprint(w, `{"id":"d1","name":"flow","elements":[]}`)
	}))
	defer backend.Close()

	if _, err := newClient(t, backend.URL).Post(context.Background(), "/drawings", map[string]any{"name": "flow"}); err != nil {
		t.Fatalf("Post: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if csrfCalls != 2 {
		t.Errorf("csrf-token fetches = %d, want 2", csrfCalls)
	}
	if writeHits != 2 {
		t.Errorf("write attempts = %d, want 2", writeHits)
	}
}

func TestFailureSurfacesStatusAndBody(t *testing.T) {
	var (
		mu        sync.Mutex
		writeHits int
	)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/csrf-token" {
			fmt.Fprint(w, `{"token":"token-1","header":"x-csrf-token"}`)
			return
		}
		mu.Lock()
		writeHits++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "boom: the backend exploded")
	}))
	defer backend.Close()

	_, err := newClient(t, backend.URL).Post(context.Background(), "/drawings", map[string]any{"name": "flow"})
	if err == nil {
		t.Fatal("Post succeeded, want a failure")
	}

	var apiErr *excalidash.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not an *excalidash.APIError", err)
	}
	if apiErr.Status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", apiErr.Status, http.StatusInternalServerError)
	}
	if !strings.Contains(apiErr.Body, "boom: the backend exploded") {
		t.Errorf("body = %q, want it to carry the backend message", apiErr.Body)
	}
	if !strings.Contains(err.Error(), "boom: the backend exploded") {
		t.Errorf("error %q, want it to carry the backend message", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if writeHits != 1 {
		t.Errorf("write attempts = %d, want 1 (a non-CSRF failure must not retry)", writeHits)
	}
}

func TestForbiddenWithoutCSRFMarkerDoesNotRetry(t *testing.T) {
	var (
		mu        sync.Mutex
		writeHits int
	)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/csrf-token" {
			fmt.Fprint(w, `{"token":"token-1","header":"x-csrf-token"}`)
			return
		}
		mu.Lock()
		writeHits++
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"detail":"not allowed"}`)
	}))
	defer backend.Close()

	if _, err := newClient(t, backend.URL).Post(context.Background(), "/drawings", map[string]any{"name": "flow"}); err == nil {
		t.Fatal("Post succeeded, want a failure")
	}

	mu.Lock()
	defer mu.Unlock()
	if writeHits != 1 {
		t.Errorf("write attempts = %d, want 1", writeHits)
	}
}

func TestSummariseDropsPreviewAndElements(t *testing.T) {
	collectionID := "c1"
	summary := excalidash.Summarise(map[string]any{
		"id":           "d1",
		"name":         "flow",
		"collectionId": collectionID,
		"createdAt":    "2026-01-01T00:00:00Z",
		"updatedAt":    "2026-01-02T00:00:00Z",
		"preview":      "<svg><rect/></svg>",
		"elements":     []any{map[string]any{"type": "rectangle"}, map[string]any{"type": "text"}},
	})

	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal(%s): %v", encoded, err)
	}
	if _, ok := decoded["preview"]; ok {
		t.Errorf("summary %s carries a preview", encoded)
	}
	if _, ok := decoded["elements"]; ok {
		t.Errorf("summary %s carries elements", encoded)
	}
	if got := decoded["elementCount"]; got != float64(2) {
		t.Errorf("elementCount = %v, want 2", got)
	}
	for _, key := range []string{"id", "name", "collectionId", "createdAt", "updatedAt"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("summary %s is missing %q", encoded, key)
		}
	}
}

func TestSummariseWithoutCollectionKeepsNull(t *testing.T) {
	encoded, err := json.Marshal(excalidash.Summarise(map[string]any{"id": "d1"}))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"collectionId":null`) {
		t.Errorf("summary %s, want a null collectionId", encoded)
	}
}

func TestElementsAcceptsJSONStringAndDecodedArray(t *testing.T) {
	array := []any{map[string]any{"type": "rectangle"}}

	for name, value := range map[string]any{
		"json string":   `[{"type":"rectangle"}]`,
		"decoded array": array,
	} {
		elements, err := excalidash.Elements(value)
		if err != nil {
			t.Errorf("Elements(%s): %v", name, err)
			continue
		}
		if len(elements) != 1 {
			t.Errorf("Elements(%s) returned %d elements, want 1", name, len(elements))
		}
	}

	for name, value := range map[string]any{
		"nil":        nil,
		"empty":      "",
		"empty list": []any{},
	} {
		elements, err := excalidash.Elements(value)
		if err != nil {
			t.Errorf("Elements(%s): %v", name, err)
			continue
		}
		if len(elements) != 0 {
			t.Errorf("Elements(%s) returned %d elements, want 0", name, len(elements))
		}
	}
}

func TestElementsRejectsNonArray(t *testing.T) {
	for name, value := range map[string]any{
		"object":      map[string]any{"type": "rectangle"},
		"json object": `{"type":"rectangle"}`,
	} {
		if _, err := excalidash.Elements(value); err == nil {
			t.Errorf("Elements(%s) succeeded, want an error", name)
		} else if !strings.Contains(err.Error(), "JSON array") {
			t.Errorf("Elements(%s) error = %q, want it to mention a JSON array", name, err)
		}
	}
}
