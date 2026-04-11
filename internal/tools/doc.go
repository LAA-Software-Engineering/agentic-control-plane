// Package tools defines tool registries and integrations (MCP, HTTP, native).
//
// [Registry] resolves tool.<name>.<operation> uses strings and dispatches MVP native and mock tools.
// Responses use [ToolCallResponse] with output + meta per §13.2.
package tools
