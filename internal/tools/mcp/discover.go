package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// ToolDescriptor is one entry from MCP tools/list.
type ToolDescriptor struct {
	Name        string
	Description string
	Meta        map[string]any
}

// ListTools connects to an MCP server, performs the initialize handshake, and returns tools/list.
func ListTools(ctx context.Context, cfg *spec.ToolMCP) ([]ToolDescriptor, error) {
	if cfg == nil {
		return nil, errors.New("mcp: nil mcp config")
	}
	trans := strings.ToLower(strings.TrimSpace(cfg.Transport))
	switch trans {
	case "stdio":
		return listToolsStdio(ctx, cfg)
	case "http":
		return listToolsHTTP(ctx, cfg, nil)
	default:
		return nil, fmt.Errorf("mcp: unsupported transport %q (stdio or http)", cfg.Transport)
	}
}

// SafetyFromDescriptors maps MCP tool descriptor meta onto merged [spec.ToolSafety].
func SafetyFromDescriptors(descriptors []ToolDescriptor) *spec.ToolSafety {
	flags := make([]*spec.ToolSafety, 0, len(descriptors))
	for _, d := range descriptors {
		flags = append(flags, spec.SafetyFromMCPMeta(d.Meta))
	}
	return spec.MergeMCPToolSafetyFlags(flags...)
}

func listToolsStdio(ctx context.Context, cfg *spec.ToolMCP) ([]ToolDescriptor, error) {
	cmd := strings.TrimSpace(cfg.Command)
	if cmd == "" {
		return nil, errors.New("mcp: stdio transport requires command")
	}
	tr := NewStdioTransport(cmd, cfg.Args)
	if err := tr.Start(ctx); err != nil {
		return nil, err
	}
	defer tr.Close()

	if err := Initialize(ctx, tr); err != nil {
		return nil, err
	}
	return listTools(ctx, tr)
}

func listToolsHTTP(ctx context.Context, cfg *spec.ToolMCP, client *http.Client) ([]ToolDescriptor, error) {
	u := strings.TrimSpace(cfg.URL)
	if u == "" {
		return nil, errors.New("mcp: http transport requires url")
	}
	tr, err := NewHTTPTransport(u, cfg.Headers, client)
	if err != nil {
		return nil, err
	}
	defer tr.Close()

	if err := Initialize(ctx, tr); err != nil {
		return nil, err
	}
	return listTools(ctx, tr)
}

func listTools(ctx context.Context, c Connector) ([]ToolDescriptor, error) {
	raw, err := c.RoundTrip(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	return parseListToolsResult(raw)
}

func parseListToolsResult(raw json.RawMessage) ([]ToolDescriptor, error) {
	var envelope struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Meta        map[string]any `json:"meta"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("mcp: decode tools/list result: %w", err)
	}
	out := make([]ToolDescriptor, 0, len(envelope.Tools))
	for _, t := range envelope.Tools {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			continue
		}
		out = append(out, ToolDescriptor{
			Name:        name,
			Description: t.Description,
			Meta:        t.Meta,
		})
	}
	return out, nil
}
