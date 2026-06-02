package policy

import (
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
)

// shellSafeRequiresApproval reports whether a tool call should be gated under shell_safe.
func shellSafeRequiresApproval(graph *spec.ProjectGraph, call ToolCallContext) bool {
	toolName, operation, err := tools.ParseUses(call.Uses)
	if err != nil {
		return true
	}
	if spec.IsShellCommandOperation(operation) {
		return spec.ShellCommandRequiresApproval(spec.ExtractShellCommand(call.With))
	}
	safety := resolvedSafetyForTool(graph, toolName)
	return safety.RequiresApproval || safety.SideEffects
}
