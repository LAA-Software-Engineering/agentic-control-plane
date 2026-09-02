package plan

import (
	"sort"
	"strings"

	"github.com/Terfyn/terfyn/internal/runtime/catalog"
	"github.com/Terfyn/terfyn/internal/spec"
)

// resolveWorkflowRuntime returns the runtime target that would execute wf: its own
// spec.runtime, else the project default, else the built-in "local" engine. This mirrors
// runtime.WorkflowRuntimeName; plan resolves it locally to keep the dependency to the leaf
// catalog package (the runtime name vocabulary), never the runtime registry.
func resolveWorkflowRuntime(g *spec.ProjectGraph, wf *spec.WorkflowResource) string {
	var explicit string
	if wf != nil {
		explicit = wf.Spec.Runtime
	}
	return effectiveRuntime(explicit, projectDefaultRuntime(g))
}

// projectDefaultRuntime is the graph's default runtime target ("" when none is set).
func projectDefaultRuntime(g *spec.ProjectGraph) string {
	if g != nil && g.Spec.Defaults != nil {
		return strings.TrimSpace(g.Spec.Defaults.Runtime)
	}
	return ""
}

// effectiveRuntime resolves an explicit workflow runtime against the project default and the
// built-in "local" fallback, so an unset runtime reads as the target that would actually run.
func effectiveRuntime(explicit, projectDefault string) string {
	if r := strings.TrimSpace(explicit); r != "" {
		return r
	}
	if r := strings.TrimSpace(projectDefault); r != "" {
		return r
	}
	return catalog.NameLocal
}

// workflowRuntimeTargets is the per-workflow resolved runtime target for the plan, in
// workflow-name order.
func workflowRuntimeTargets(g *spec.ProjectGraph) []RuntimeTargetItem {
	if g == nil || len(g.Workflows) == 0 {
		return nil
	}
	names := make([]string, 0, len(g.Workflows))
	for name := range g.Workflows {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]RuntimeTargetItem, 0, len(names))
	for _, name := range names {
		out = append(out, RuntimeTargetItem{Workflow: name, Runtime: resolveWorkflowRuntime(g, g.Workflows[name])})
	}
	return out
}
