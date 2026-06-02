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
		cmd := spec.ExtractShellCommand(call.With)
		token := spec.FirstShellToken(cmd)
		switch spec.ClassifyShellToken(token) {
		case spec.ShellTokenReadOnly:
			return false
		case spec.ShellTokenGate, spec.ShellTokenUnknown:
			return true
		}
	}
	safety := resolvedSafetyForTool(graph, toolName)
	if safety.RequiresApproval || safety.SideEffects {
		return true
	}
	return false
}
