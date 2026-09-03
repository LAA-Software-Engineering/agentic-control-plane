// Package anthropic implements the Anthropic Messages API client (design doc §7.1, issue #69).
package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.anthropic.com"
	apiVersion     = "2023-06-01"
	defaultMaxTok  = 4096
)

// Client calls POST /v1/messages.
type Client struct {
	APIKey  string
	BaseURL string
	// WorkspaceID, when set, is sent as the anthropic-workspace-id header. It is
	// required when authenticating with an identity-linked API key (which is not
	// scoped to a single workspace); a plain workspace-scoped key leaves it empty.
	WorkspaceID string
	HTTPClient  *http.Client
}

func (c *Client) base() string {
	if c != nil && strings.TrimSpace(c.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	}
	return defaultBaseURL
}

func (c *Client) http() *http.Client {
	if c != nil && c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// Generate performs one non-streaming Messages request. System may be empty.
// Tools / tool_choice are omitted when unset. ToolCalls are populated only when
// StopReason is tool_use.
func (c *Client) Generate(ctx context.Context, req Request) (Response, error) {
	if c == nil || strings.TrimSpace(c.APIKey) == "" {
		return Response{}, fmt.Errorf("anthropic: client not configured")
	}
	start := time.Now()

	body, err := marshalRequest(req)
	if err != nil {
		return Response{}, err
	}
	url := c.base() + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.APIKey)
	httpReq.Header.Set("anthropic-version", apiVersion)
	if ws := strings.TrimSpace(c.WorkspaceID); ws != "" {
		httpReq.Header.Set("anthropic-workspace-id", ws)
	}

	resp, err := c.http().Do(httpReq)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, err
	}
	durationMs := time.Since(start).Milliseconds()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{DurationMs: durationMs}, fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, truncateErrBody(b))
	}

	out, err := parseResponse(b)
	if err != nil {
		out.DurationMs = durationMs
		return out, err
	}
	out.DurationMs = durationMs
	return out, nil
}

func truncateErrBody(b []byte) string {
	const n = 500
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
