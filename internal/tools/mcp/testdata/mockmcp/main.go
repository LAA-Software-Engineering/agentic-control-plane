// Mock MCP stdio server for tests: handles initialize and tools/call by echoing arguments as JSON text.
package main

import (
	"bufio"
	"encoding/json"
	"os"
)

func main() {
	sc := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		line := sc.Bytes()
		var msg map[string]any
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		method, _ := msg["method"].(string)
		if msg["id"] == nil {
			continue
		}
		id := msg["id"]
		switch method {
		case "initialize":
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "mockmcp", "version": "1"},
				},
			})
		case "tools/call":
			params, _ := msg["params"].(map[string]any)
			args := map[string]any{}
			if params != nil {
				if a, ok := params["arguments"].(map[string]any); ok {
					args = a
				}
			}
			b, _ := json.Marshal(args)
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"content": []any{map[string]any{"type": "text", "text": string(b)}},
				},
			})
		default:
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"error": map[string]any{
					"code":    -32601,
					"message": "method not found",
				},
			})
		}
	}
}
