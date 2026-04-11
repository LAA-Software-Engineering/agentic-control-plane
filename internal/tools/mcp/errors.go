package mcp

import "fmt"

// rpcError is a JSON-RPC error returned by the MCP server (do not retry as transport).
type rpcError struct {
	detail string
}

func (e *rpcError) Error() string {
	return "mcp: " + e.detail
}

func rpcErrorf(format string, args ...any) error {
	return &rpcError{detail: fmt.Sprintf(format, args...)}
}
