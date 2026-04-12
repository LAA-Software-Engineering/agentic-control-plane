// Package anthropic implements the Anthropic Messages API client (design doc §7.1, issue #69).
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
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

// ChatMessage is one user or assistant turn for the Messages API (roles user|assistant only).
type ChatMessage struct {
	Role    string
	Content string
}

// Client calls POST /v1/messages.
type Client struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
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

// Generate performs one non-streaming Messages request. system may be empty.
// Returns assistant text (concatenated text blocks), token usage when present, and duration in ms.
func (c *Client) Generate(ctx context.Context, model, system string, messages []ChatMessage) (text string, inputTokens, outputTokens int, durationMs int64, err error) {
	if c == nil || strings.TrimSpace(c.APIKey) == "" {
		return "", 0, 0, 0, fmt.Errorf("anthropic: client not configured")
	}
	if strings.TrimSpace(model) == "" {
		return "", 0, 0, 0, fmt.Errorf("anthropic: empty model")
	}
	start := time.Now()

	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	payload := struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		System    string `json:"system,omitempty"`
		Messages  []msg  `json:"messages"`
	}{
		Model:     model,
		MaxTokens: defaultMaxTok,
		System:    strings.TrimSpace(system),
	}
	for _, m := range messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role != "user" && role != "assistant" {
			return "", 0, 0, 0, fmt.Errorf("anthropic: message role %q is not user or assistant", m.Role)
		}
		payload.Messages = append(payload.Messages, msg{Role: role, Content: m.Content})
	}
	if len(payload.Messages) == 0 {
		return "", 0, 0, 0, fmt.Errorf("anthropic: no messages")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", 0, 0, 0, err
	}
	url := c.base() + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.APIKey)
	httpReq.Header.Set("anthropic-version", apiVersion)

	resp, err := c.http().Do(httpReq)
	if err != nil {
		return "", 0, 0, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, 0, 0, err
	}
	durationMs = time.Since(start).Milliseconds()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, 0, durationMs, fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, truncateErrBody(b))
	}

	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", 0, 0, durationMs, fmt.Errorf("anthropic: decode response: %w", err)
	}
	var parts []string
	for _, block := range out.Content {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	text = strings.Join(parts, "")
	if text == "" {
		return "", 0, 0, durationMs, fmt.Errorf("anthropic: no text content in response")
	}
	if out.Usage != nil {
		inputTokens = out.Usage.InputTokens
		outputTokens = out.Usage.OutputTokens
	}
	return text, inputTokens, outputTokens, durationMs, nil
}

func truncateErrBody(b []byte) string {
	const n = 500
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
