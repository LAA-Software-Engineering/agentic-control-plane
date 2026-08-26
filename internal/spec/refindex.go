package spec

import "strings"

// RefIndex summarizes symbolic references between resources (issue #6, §9.1).
type RefIndex struct {
	AgentTools        map[string][]string
	AgentPolicies     map[string]string
	WorkflowAgents    map[string][]string
	WorkflowTools     map[string][]string
	WorkflowWorkflows map[string][]string
	WorkflowPolicies  map[string]string
}

// BuildRefIndex scans ProjectGraph resources and builds RefIndex lookup tables.
func BuildRefIndex(g *ProjectGraph) *RefIndex {
	if g == nil {
		return &RefIndex{
			AgentTools:        map[string][]string{},
			AgentPolicies:     map[string]string{},
			WorkflowAgents:    map[string][]string{},
			WorkflowTools:     map[string][]string{},
			WorkflowWorkflows: map[string][]string{},
			WorkflowPolicies:  map[string]string{},
		}
	}
	ix := &RefIndex{
		AgentTools:        make(map[string][]string),
		AgentPolicies:     make(map[string]string),
		WorkflowAgents:    make(map[string][]string),
		WorkflowTools:     make(map[string][]string),
		WorkflowWorkflows: make(map[string][]string),
		WorkflowPolicies:  make(map[string]string),
	}
	for name, ar := range g.Agents {
		if ar == nil {
			continue
		}
		var tools []string
		for _, t := range ar.Spec.Tools {
			s := strings.TrimSpace(t)
			if s == "" {
				continue
			}
			if tn, ok := ParseToolUses(s); ok {
				tools = append(tools, tn)
				continue
			}
			tools = append(tools, s)
		}
		ix.AgentTools[name] = dedupeRefStrings(tools)
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
		var agents, tools, workflows []string
		for _, st := range wr.Spec.Steps {
			if a := strings.TrimSpace(st.Agent); a != "" {
				agents = append(agents, a)
			}
			if u := strings.TrimSpace(st.Uses); u != "" {
				if tn, ok := ParseToolUses(u); ok {
					tools = append(tools, tn)
				}
			}
			if w := strings.TrimSpace(st.Workflow); w != "" {
				workflows = append(workflows, w)
			}
		}
		ix.WorkflowAgents[name] = dedupeRefStrings(agents)
		ix.WorkflowTools[name] = dedupeRefStrings(tools)
		ix.WorkflowWorkflows[name] = dedupeRefStrings(workflows)
	}
	return ix
}

func dedupeRefStrings(in []string) []string {
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
