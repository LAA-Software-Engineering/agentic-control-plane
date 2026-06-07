package runtime

import (
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// WorkflowRuntimeName returns the effective runtime for a workflow after defaults are applied.
func WorkflowRuntimeName(g *spec.ProjectGraph, wfName string) string {
	def := NameLocal
	if g != nil && g.Spec.Defaults != nil {
		if r := strings.TrimSpace(g.Spec.Defaults.Runtime); r != "" {
			def = r
		}
	}
	if g == nil || g.Workflows == nil {
		return def
	}
	wf, ok := g.Workflows[strings.TrimSpace(wfName)]
	if !ok || wf == nil {
		return def
	}
	if r := strings.TrimSpace(wf.Spec.Runtime); r != "" {
		return r
	}
	return def
}
