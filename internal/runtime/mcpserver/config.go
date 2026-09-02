package mcpserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Transport describes how the spawned external agent reaches the per-run server. MCP supports
// a stdio server (the agent launches Command with Args and speaks JSON-RPC over its stdio) and
// an HTTP server (the agent connects to URL). Exactly one form is populated.
type Transport struct {
	// stdio transport
	Command string
	Args    []string
	// http transport (e.g. a loopback endpoint from Server.ListenLocal)
	URL string
	// Headers are sent on every HTTP request to URL — for the per-run bearer token that
	// authenticates the endpoint (e.g. {"Authorization": "Bearer <token>"}). Claude Code's
	// --mcp-config honors http headers.
	Headers map[string]string
}

// MCPConfigJSON renders the --mcp-config document that registers the per-run server under
// serverName. Passed with --strict-mcp-config, it is the *only* MCP server the external agent
// sees, so the advertised tools are exactly this run's grants.
func MCPConfigJSON(serverName string, t Transport) ([]byte, error) {
	if serverName == "" {
		serverName = "terfyn"
	}
	var entry map[string]any
	switch {
	case t.URL != "":
		entry = map[string]any{"type": "http", "url": t.URL}
		if len(t.Headers) > 0 {
			entry["headers"] = t.Headers
		}
	case t.Command != "":
		entry = map[string]any{"type": "stdio", "command": t.Command, "args": t.Args}
	default:
		return nil, fmt.Errorf("mcpserver: transport has neither Command nor URL")
	}
	doc := map[string]any{"mcpServers": map[string]any{serverName: entry}}
	return json.MarshalIndent(doc, "", "  ")
}

// WriteMCPConfig writes the config document into dir and returns its path, for passing to the
// external CLI as --mcp-config. The file is created 0600 (it may name a local endpoint).
func WriteMCPConfig(dir, serverName string, t Transport) (string, error) {
	b, err := MCPConfigJSON(serverName, t)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "mcp-config.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", fmt.Errorf("mcpserver: write mcp config: %w", err)
	}
	return path, nil
}
