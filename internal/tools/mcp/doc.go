// Package mcp implements MCP tool transport per design doc §7.3.
//
// Call runs tools/call over stdio (subprocess) or streamable HTTP per spec.mcp.transport (issue #77):
// initialize + notifications/initialized, then tools/call. HTTPS uses the Go default HTTP transport
// with standard TLS certificate verification.
package mcp
