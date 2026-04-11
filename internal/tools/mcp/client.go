package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// Initialize performs the MCP initialize + notifications/initialized handshake.
func (t *StdioTransport) Initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "agentic-control-plane",
			"version": "0",
		},
	}
	if _, err := t.RoundTrip(ctx, "initialize", params); err != nil {
		return err
	}
	// Notification (no response).
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.writeMessage(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	})
}

// CallTool invokes tools/call and maps the MCP result into a plain map for §13.2 output.
func (t *StdioTransport) CallTool(ctx context.Context, name string, arguments map[string]any) (map[string]any, error) {
	if arguments == nil {
		arguments = map[string]any{}
	}
	raw, err := t.RoundTrip(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": arguments,
	})
	if err != nil {
		return nil, err
	}
	return parseCallToolResult(raw)
}

func parseCallToolResult(raw json.RawMessage) (map[string]any, error) {
	var envelope struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("mcp: decode tools/call result: %w", err)
	}
	if envelope.IsError {
		return nil, rpcErrorf("tools/call isError=true: %s", string(raw))
	}
	if len(envelope.Content) == 0 {
		return map[string]any{}, nil
	}
	first := envelope.Content[0]
	if first.Type == "text" && first.Text != "" {
		var obj map[string]any
		if json.Unmarshal([]byte(first.Text), &obj) == nil {
			return obj, nil
		}
		return map[string]any{"text": first.Text}, nil
	}
	return map[string]any{"content": envelope.Content}, nil
}
