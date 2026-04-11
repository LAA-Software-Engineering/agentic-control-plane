// Package mcp implements MCP tool transport (MVP: stdio JSON-RPC) per design doc §7.3.
//
// [CallStdio] runs a subprocess, performs initialize / notifications/initialized, then tools/call.
package mcp
