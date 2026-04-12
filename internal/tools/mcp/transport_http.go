package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
)

// Streamable HTTP transport (MCP spec): one POST per JSON-RPC message to a single MCP endpoint.
// Supports application/json responses and basic text/event-stream (SSE) data lines.
const mcpHTTPProtocolVersion = "2024-11-05"

// HTTPTransport performs MCP over HTTP POST to one URL (issue #77).
type HTTPTransport struct {
	endpoint   *url.URL
	headerBase http.Header
	client     *http.Client

	mu        sync.Mutex
	sessionID string
	nextID    int64
}

// NewHTTPTransport validates url (http/https only) and resolves headers (env: tokens like HTTP tools).
func NewHTTPTransport(rawURL string, headers map[string]string, client *http.Client) (*HTTPTransport, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("mcp: empty url")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("mcp: invalid url %q", rawURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("mcp: url scheme must be http or https, got %q", u.Scheme)
	}
	hdr, err := resolveMCPHTTPHeaders(headers)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPTransport{endpoint: u, headerBase: hdr, client: client}, nil
}

func resolveMCPHTTPHeaders(h map[string]string) (http.Header, error) {
	out := make(http.Header)
	if h == nil {
		return out, nil
	}
	for k, v := range h {
		resolved, err := resolveMCPHeaderValue(v)
		if err != nil {
			return nil, fmt.Errorf("mcp: header %q: %w", k, err)
		}
		out.Set(k, resolved)
	}
	return out, nil
}

func resolveMCPHeaderValue(v string) (string, error) {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "env:") {
		return models.ResolveAPIKeyFrom(v)
	}
	return v, nil
}

// Close is a no-op; the transport does not own persistent connections beyond the client.
func (t *HTTPTransport) Close() error { return nil }

func (t *HTTPTransport) cloneRequestHeaders() http.Header {
	h := t.headerBase.Clone()
	h.Set("Accept", "application/json, text/event-stream")
	h.Set("Content-Type", "application/json")
	h.Set("MCP-Protocol-Version", mcpHTTPProtocolVersion)
	t.mu.Lock()
	sid := t.sessionID
	t.mu.Unlock()
	if sid != "" {
		h.Set("Mcp-Session-Id", sid)
	}
	return h
}

func (t *HTTPTransport) captureSession(resp *http.Response) {
	if sid := strings.TrimSpace(resp.Header.Get("Mcp-Session-Id")); sid != "" {
		t.mu.Lock()
		t.sessionID = sid
		t.mu.Unlock()
	}
}

// Notify sends a JSON-RPC notification; servers SHOULD respond with 202 Accepted and no body.
func (t *HTTPTransport) Notify(ctx context.Context, method string, params map[string]any) error {
	if params == nil {
		params = map[string]any{}
	}
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint.String(), bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header = t.cloneRequestHeaders()

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	t.captureSession(resp)

	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusOK {
		return nil
	}
	return fmt.Errorf("mcp: notification HTTP %s", resp.Status)
}

// RoundTrip sends one JSON-RPC request and returns the result for the matching id.
func (t *HTTPTransport) RoundTrip(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := atomic.AddInt64(&t.nextID, 1)
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint.String(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header = t.cloneRequestHeaders()

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	t.captureSession(resp)

	if resp.StatusCode == http.StatusAccepted {
		return nil, fmt.Errorf("mcp: unexpected 202 for JSON-RPC request with id (method %q)", method)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mcp: HTTP %s: %s", resp.Status, truncateForErr(body, 512))
	}
	ct := resp.Header.Get("Content-Type")
	raw, err := parseJSONRPCHTTPResponseBody(body, ct, id)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func truncateForErr(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func parseJSONRPCHTTPResponseBody(body []byte, contentType string, wantID int64) (json.RawMessage, error) {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "text/event-stream") {
		return parseJSONRPCFromSSE(body, wantID)
	}
	var msg map[string]any
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, fmt.Errorf("mcp: decode JSON-RPC: %w", err)
	}
	return jsonRPCResultFromMapStrict(msg, wantID)
}

func parseJSONRPCFromSSE(body []byte, wantID int64) (json.RawMessage, error) {
	s := string(body)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			continue
		}
		raw, matched, err := jsonRPCResultFromMap(msg, wantID)
		if err != nil {
			return nil, err
		}
		if matched {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("mcp: no JSON-RPC result with matching id in SSE response")
}
