package scaffold

import (
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// Tool kinds supported by agentctl new tool.
const (
	ToolKindNative = "native"
	ToolKindHTTP   = "http"
	ToolKindMock   = "mock"
	ToolKindMCP    = "mcp"
)

// ToolKindNames returns sorted valid tool kinds for scaffolding.
func ToolKindNames() []string {
	return []string{ToolKindHTTP, ToolKindMCP, ToolKindMock, ToolKindNative}
}

func renderToolYAML(name, kind string) ([]byte, error) {
	name = strings.TrimSpace(name)
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case ToolKindNative:
		return []byte(fmt.Sprintf(`apiVersion: %s
kind: Tool
metadata:
  name: %s
spec:
  type: native
  safety:
    sideEffects: false
`, spec.APIVersionV0, name)), nil
	case ToolKindMock:
		return []byte(fmt.Sprintf(`apiVersion: %s
kind: Tool
metadata:
  name: %s
spec:
  type: mock
  safety:
    sideEffects: false
`, spec.APIVersionV0, name)), nil
	case ToolKindHTTP:
		return []byte(fmt.Sprintf(`apiVersion: %s
kind: Tool
metadata:
  name: %s
spec:
  type: http
  http:
    baseUrl: https://api.example.com
  safety:
    sideEffects: true
`, spec.APIVersionV0, name)), nil
	case ToolKindMCP:
		return []byte(fmt.Sprintf(`apiVersion: %s
kind: Tool
metadata:
  name: %s
spec:
  type: mcp
  mcp:
    transport: stdio
    command: npx
    args:
      - -y
      - "@modelcontextprotocol/server-example"
  safety:
    sideEffects: true
`, spec.APIVersionV0, name)), nil
	default:
		return nil, fmt.Errorf("scaffold: unsupported tool kind %q (want %s)", kind, strings.Join(ToolKindNames(), ", "))
	}
}

func renderPolicyYAML(name, preset string) ([]byte, error) {
	name = strings.TrimSpace(name)
	preset = strings.TrimSpace(preset)
	if !spec.IsBuiltinPreset(preset) {
		return nil, &spec.ErrUnknownPreset{Name: preset}
	}
	return []byte(fmt.Sprintf(`apiVersion: %s
kind: Policy
metadata:
  name: %s
spec:
  preset: %s
  execution:
    maxWallClockSeconds: 300
    maxTotalCostUsd: 5
`, spec.APIVersionV0, name, preset)), nil
}

func renderWorkflowYAML(name, policy string) []byte {
	name = strings.TrimSpace(name)
	policy = strings.TrimSpace(policy)
	if policy == "" {
		policy = "default"
	}
	return []byte(fmt.Sprintf(`apiVersion: %s
kind: Workflow
metadata:
  name: %s
spec:
  policy: %s
  steps: []
`, spec.APIVersionV0, name, policy))
}

func renderAgentYAML(name string) []byte {
	name = strings.TrimSpace(name)
	return []byte(fmt.Sprintf(`apiVersion: %s
kind: Agent
metadata:
  name: %s
spec: {}
`, spec.APIVersionV0, name))
}
