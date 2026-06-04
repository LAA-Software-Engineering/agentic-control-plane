package engine

import (
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
)

func (e *Executor) toolSafetyForUses(uses string) spec.ResolvedToolSafety {
	if e == nil || e.Graph == nil {
		return spec.ResolveToolSafety(nil)
	}
	toolName, _, err := tools.ParseUses(uses)
	if err != nil {
		return spec.ResolveToolSafety(nil)
	}
	tr, ok := e.Graph.Tools[toolName]
	if !ok || tr == nil {
		return spec.ResolveToolSafety(nil)
	}
	return spec.ResolveToolSafety(tr.Spec.Safety)
}
