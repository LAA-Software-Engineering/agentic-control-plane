package project

import (
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// RefIndex summarizes symbolic references between resources before resolution (issue #6).
// Names are metadata.name values; edges point to target kinds as implied by the field.
type RefIndex struct {
	// AgentName -> tool names from Agent.spec.tools
	AgentTools map[string][]string
	// AgentName -> policy name when Agent.spec.policy is non-empty
	AgentPolicies map[string]string
	// WorkflowName -> agent names from steps with agent: set
	WorkflowAgents map[string][]string
	// WorkflowName -> tool names from steps with uses: tool.<name>...
	WorkflowTools map[string][]string
	// WorkflowName -> policy name when Workflow.spec.policy is non-empty
	WorkflowPolicies map[string]string
}

// BuildRefIndex scans ProjectGraph resources and builds RefIndex lookup tables.
func BuildRefIndex(g *spec.ProjectGraph) *RefIndex {
	if g == nil {
		return &RefIndex{
			AgentTools:       map[string][]string{},
			AgentPolicies:    map[string]string{},
			WorkflowAgents:   map[string][]string{},
			WorkflowTools:    map[string][]string{},
			WorkflowPolicies: map[string]string{},
		}
	}
	ix := &RefIndex{
		AgentTools:       make(map[string][]string),
		AgentPolicies:    make(map[string]string),
		WorkflowAgents:   make(map[string][]string),
		WorkflowTools:    make(map[string][]string),
		WorkflowPolicies: make(map[string]string),
	}
	for name, ar := range g.Agents {
		if ar == nil {
			continue
		}
		var tools []string
		for _, t := range ar.Spec.Tools {
			if s := strings.TrimSpace(t); s != "" {
				tools = append(tools, s)
			}
		}
		ix.AgentTools[name] = dedupeStrings(tools)
		if p := strings.TrimSpace(ar.Spec.Policy); p != "" {
			ix.AgentPolicies[name] = p
		}
	}
	for name, wr := range g.Workflows {
		if wr == nil {
			continue
		}
		if p := strings.TrimSpace(wr.Spec.Policy); p != "" {
			ix.WorkflowPolicies[name] = p
		}
		var agents, tools []string
		for _, st := range wr.Spec.Steps {
			if a := strings.TrimSpace(st.Agent); a != "" {
				agents = append(agents, a)
			}
			if u := strings.TrimSpace(st.Uses); u != "" {
				if tn, ok := spec.ParseToolUses(u); ok {
					tools = append(tools, tn)
				}
			}
		}
		ix.WorkflowAgents[name] = dedupeStrings(agents)
		ix.WorkflowTools[name] = dedupeStrings(tools)
	}
	return ix
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
