// Package tools defines tool registries and integrations (MCP, HTTP, native).
//
// [Registry] resolves tool.<name>.<operation> uses strings and dispatches MVP native, mock, MCP stdio, and HTTP tools.
// [ApplyMCPSafetyDiscovery] merges MCP meta.mcp_flags into Tool safety during config resolution (issue #125).
// Responses use [ToolCallResponse] with output + meta per §13.2.
package tools
