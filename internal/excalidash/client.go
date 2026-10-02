// Package excalidash provides a client for the Excalidash REST API.
package excalidash

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is the in-cluster address of the Excalidash backend.
const DefaultBaseURL = "http://excalidash-backend.excalidash.svc.cluster.local:8000"

// DefaultOrigin is the origin the Excalidash backend accepts writes from.
const DefaultOrigin = "https://draw.maklab.net"

// csrfPath is the endpoint that mints CSRF tokens.
const csrfPath = "/csrf-token"

// csrfHeaderFallback is used until the API names its own CSRF header.
const csrfHeaderFallback = "x-csrf-token"

// maxErrorBody caps how much of a failure body is surfaced to a caller.
const maxErrorBody = 400

// Client calls the Excalidash REST API.
type Client struct {
	baseURL string
	origin  string
	http    *http.Client

	mu         sync.Mutex
	csrfHeader string
	csrfToken  string
}

// New returns a Client that sends origin with every request to baseURL.
func New(baseURL, origin string) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		origin:     origin,
		http:       &http.Client{Jar: jar, Timeout: 60 * time.Second},
		csrfHeader: csrfHeaderFallback,
	}, nil
}

// Get returns the decoded JSON body of a GET request.
func (c *Client) Get(ctx context.Context, path string) (any, error) {
	return c.do(ctx, http.MethodGet, path, nil)
}

// Post returns the decoded JSON body of a POST request carrying payload.
func (c *Client) Post(ctx context.Context, path string, payload any) (any, error) {
	return c.do(ctx, http.MethodPost, path, payload)
}

// Put returns the decoded JSON body of a PUT request carrying payload.
func (c *Client) Put(ctx context.Context, path string, payload any) (any, error) {
	return c.do(ctx, http.MethodPut, path, payload)
}

// Delete returns the decoded JSON body of a DELETE request.
func (c *Client) Delete(ctx context.Context, path string) (any, error) {
	return c.do(ctx, http.MethodDelete, path, nil)
}

// do performs one API call, refreshing a stale CSRF token at most once.
func (c *Client) do(ctx context.Context, method, path string, payload any) (any, error) {
	if method != http.MethodGet {
		if err := c.refreshCSRF(ctx); err != nil {
			return nil, err
		}
	}

	body, err := encodePayload(payload)
	if err != nil {
		return nil, fmt.Errorf("encode %s %s payload: %w", method, path, err)
	}

	value, err := c.attempt(ctx, method, path, body)
	if err == nil {
		return value, nil
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) || !apiErr.staleCSRF() {
		return nil, err
	}
	if err := c.refreshCSRF(ctx); err != nil {
		return nil, err
	}
	return c.attempt(ctx, method, path, body)
}

// attempt sends a single request and decodes its JSON body.
func (c *Client) attempt(ctx context.Context, method, path string, body []byte) (any, error) {
	request, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer response.Body.Close()

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s %s response: %w", method, path, err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, &APIError{Method: method, Path: path, Status: response.StatusCode, Body: string(raw)}
	}
	if len(raw) == 0 {
		return map[string]any{}, nil
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("decode %s %s response: %w", method, path, err)
	}
	return value, nil
}

// newRequest builds a request carrying the origin and, for writes, the CSRF header.
func (c *Client) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("build %s %s request: %w", method, path, err)
	}
	request.Header.Set("Origin", c.origin)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		header, token := c.csrf()
		request.Header.Set(header, token)
	}
	return request, nil
}

// refreshCSRF fetches a token and keeps the cookie it is bound to.
func (c *Client) refreshCSRF(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+csrfPath, nil)
	if err != nil {
		return fmt.Errorf("build CSRF request: %w", err)
	}
	request.Header.Set("Origin", c.origin)
	request.Header.Set("Accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("fetch CSRF token: %w", err)
	}
	defer response.Body.Close()

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read CSRF token response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return &APIError{Method: http.MethodGet, Path: csrfPath, Status: response.StatusCode, Body: string(raw)}
	}

	var body struct {
		Token  string `json:"token"`
		Header string `json:"header"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("decode CSRF token response: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if body.Header != "" {
		c.csrfHeader = body.Header
	}
	c.csrfToken = body.Token
	return nil
}

// csrf returns the current CSRF header name and token.
func (c *Client) csrf() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.csrfHeader, c.csrfToken
}

// encodePayload marshals a request payload, returning nil when there is none.
func encodePayload(payload any) ([]byte, error) {
	if payload == nil {
		return nil, nil
	}
	return json.Marshal(payload)
}

// APIError is a non-2xx response from the Excalidash API.
type APIError struct {
	Method string
	Path   string
	Status int
	Body   string
}

// Error renders the status and body of a failed call.
func (e *APIError) Error() string {
	body := e.Body
	if len(body) > maxErrorBody {
		body = body[:maxErrorBody]
	}
	return fmt.Sprintf("HTTP %d %s %s: %s", e.Status, e.Method, e.Path, body)
}

// staleCSRF reports whether the API rejected the call for a bad CSRF token.
func (e *APIError) staleCSRF() bool {
	return e.Status == http.StatusForbidden && strings.Contains(e.Body, "CSRF")
}
