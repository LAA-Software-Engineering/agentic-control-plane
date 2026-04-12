package policy

import (
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
)

func checkKnownTool(graph *spec.ProjectGraph, uses string, toolsPol *spec.PolicyTools) error {
	if toolsPol == nil || !toolsPol.ForbidUnknownTools {
		return nil
	}
	toolName, _, err := tools.ParseUses(uses)
	if err != nil {
		return denied(ReasonInvalidUses, fmt.Sprintf("policy: %v", err), uses, nil)
	}
	if graph == nil || graph.Tools == nil {
		return denied(
			ReasonUnknownTool,
			fmt.Sprintf("policy: unknown tool %q (forbidUnknownTools)", toolName),
			uses,
			map[string]any{"tool": toolName},
		)
	}
	if _, ok := graph.Tools[toolName]; !ok {
		return denied(
			ReasonUnknownTool,
			fmt.Sprintf("policy: unknown tool %q (forbidUnknownTools)", toolName),
			uses,
			map[string]any{"tool": toolName},
		)
	}
	return nil
}
