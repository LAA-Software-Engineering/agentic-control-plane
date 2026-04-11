package spec

import "strings"

// NormalizeProjectGraph applies Project.spec.defaults to resources that omit matching
// fields and performs trivial string canonicalization (trim surrounding ASCII space).
//
// Default application (§7.1 → effective config):
//   - Agent.spec.model  ← defaults.model when the agent omits model (empty / whitespace-only).
//   - Agent.spec.policy ← defaults.policy when the agent omits policy.
//   - Workflow.spec.policy ← defaults.policy when the workflow omits policy.
//
// defaults.runtime: MVP Agent and Workflow specs have no runtime field (§7.2, §7.4), so
// this value is not copied onto resources here; a future loader may attach it when a
// target field exists.
//
// Environment overrides are out of scope (issue #4). Mutates graphs in place.
func NormalizeProjectGraph(g *ProjectGraph) {
	if g == nil {
		return
	}
	def := readProjectDefaults(g)
	_ = def.Runtime // reserved until a spec field consumes it

	for _, a := range g.Agents {
		if a == nil {
			continue
		}
		normalizeAgentSpec(&a.Spec, def.Model, def.Policy)
	}
	for _, w := range g.Workflows {
		if w == nil {
			continue
		}
		normalizeWorkflowSpec(&w.Spec, def.Policy)
	}
}

func normalizeAgentSpec(spec *AgentSpec, defModel, defPolicy string) {
	if spec == nil {
		return
	}
	// Model: default when omitted; otherwise trim only.
	if defModel != "" && isOmitted(spec.Model) {
		spec.Model = defModel
	} else {
		spec.Model = strings.TrimSpace(spec.Model)
	}
	// Policy: default when omitted; otherwise trim only.
	if defPolicy != "" && isOmitted(spec.Policy) {
		spec.Policy = defPolicy
	} else {
		spec.Policy = strings.TrimSpace(spec.Policy)
	}
}

func normalizeWorkflowSpec(spec *WorkflowSpec, defPolicy string) {
	if spec == nil {
		return
	}
	if defPolicy != "" && isOmitted(spec.Policy) {
		spec.Policy = defPolicy
	} else {
		spec.Policy = strings.TrimSpace(spec.Policy)
	}
}

func isOmitted(s string) bool {
	return strings.TrimSpace(s) == ""
}
