package spec

import "strings"

// NormalizeProjectGraph applies Project.spec.defaults to resources that omit matching
// fields, materializes Tool safety defaults (issue #103), expands built-in policy presets
// (issue #104), and performs trivial string canonicalization (trim surrounding ASCII space).
//
// MCP tool safety from server meta.mcp_flags is merged earlier in the config pipeline via
// [tools.ApplyMCPSafetyDiscovery] before this function runs (issue #125).
//
// Default application (§7.1 → effective config):
//   - Agent.spec.model    ← defaults.model when the agent omits model (empty / whitespace-only).
//   - Agent.spec.policy   ← defaults.policy when the agent omits policy.
//   - Agent.spec.runtime  ← defaults.runtime when the agent omits runtime (issue #76).
//   - Workflow.spec.policy  ← defaults.policy when the workflow omits policy.
//   - Workflow.spec.runtime ← defaults.runtime when the workflow omits runtime (issue #76).
//
// Environment overlays (design doc §7.6) are not applied here. Typical pipelines load the graph,
// run NormalizeProjectGraph, then apply the selected environment with [spec.ApplyEnvironment] in
// the control plane before handing an immutable snapshot to [runtime.Runtime], then validate.
// Mutates g in place.
func NormalizeProjectGraph(g *ProjectGraph) {
	if g == nil {
		return
	}
	def := readProjectDefaults(g)

	for _, a := range g.Agents {
		if a == nil {
			continue
		}
		normalizeAgentSpec(&a.Spec, def.Model, def.Policy, def.Runtime)
	}
	for _, w := range g.Workflows {
		if w == nil {
			continue
		}
		normalizeWorkflowSpec(&w.Spec, def.Policy, def.Runtime)
	}
	for _, tr := range g.Tools {
		if tr == nil {
			continue
		}
		NormalizeToolSafety(&tr.Spec)
	}
	ExpandPresetsInGraph(g)
}

func normalizeAgentSpec(spec *AgentSpec, defModel, defPolicy, defRuntime string) {
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
	if defRuntime != "" && isOmitted(spec.Runtime) {
		spec.Runtime = defRuntime
	} else {
		spec.Runtime = strings.TrimSpace(spec.Runtime)
	}
}

func normalizeWorkflowSpec(spec *WorkflowSpec, defPolicy, defRuntime string) {
	if spec == nil {
		return
	}
	if defPolicy != "" && isOmitted(spec.Policy) {
		spec.Policy = defPolicy
	} else {
		spec.Policy = strings.TrimSpace(spec.Policy)
	}
	if defRuntime != "" && isOmitted(spec.Runtime) {
		spec.Runtime = defRuntime
	} else {
		spec.Runtime = strings.TrimSpace(spec.Runtime)
	}
}

func isOmitted(s string) bool {
	return strings.TrimSpace(s) == ""
}
