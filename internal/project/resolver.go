package project

import (
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// ResolveReferences checks that symbolic references in the merged graph resolve to
// existing resources (§9.1) and applies simple sequential workflow rules (§9.4).
func ResolveReferences(g *spec.ProjectGraph) error {
	if g == nil {
		return nil
	}
	ix := BuildRefIndex(g)

	for agentName, tools := range ix.AgentTools {
		for _, tn := range tools {
			if _, ok := g.Tools[tn]; !ok {
				return &MissingRefError{
					Referrer: spec.ResourceID{Kind: spec.KindAgent, Name: agentName},
					Missing:  spec.ResourceID{Kind: spec.KindTool, Name: tn},
				}
			}
		}
	}
	for agentName, pol := range ix.AgentPolicies {
		if _, ok := g.Policies[pol]; !ok {
			return &MissingRefError{
				Referrer: spec.ResourceID{Kind: spec.KindAgent, Name: agentName},
				Missing:  spec.ResourceID{Kind: spec.KindPolicy, Name: pol},
			}
		}
	}

	for wfName, wr := range g.Workflows {
		if wr == nil {
			continue
		}
		if err := validateWorkflowSteps(wfName, &wr.Spec); err != nil {
			return err
		}
		for _, an := range ix.WorkflowAgents[wfName] {
			if _, ok := g.Agents[an]; !ok {
				return &MissingRefError{
					Referrer: spec.ResourceID{Kind: spec.KindWorkflow, Name: wfName},
					Missing:  spec.ResourceID{Kind: spec.KindAgent, Name: an},
				}
			}
		}
		for _, tn := range ix.WorkflowTools[wfName] {
			if _, ok := g.Tools[tn]; !ok {
				return &MissingRefError{
					Referrer: spec.ResourceID{Kind: spec.KindWorkflow, Name: wfName},
					Missing:  spec.ResourceID{Kind: spec.KindTool, Name: tn},
				}
			}
		}
		if pol := ix.WorkflowPolicies[wfName]; pol != "" {
			if _, ok := g.Policies[pol]; !ok {
				return &MissingRefError{
					Referrer: spec.ResourceID{Kind: spec.KindWorkflow, Name: wfName},
					Missing:  spec.ResourceID{Kind: spec.KindPolicy, Name: pol},
				}
			}
		}
		if err := validateWorkflowStepOrder(wfName, &wr.Spec); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkflowSteps(wfName string, w *spec.WorkflowSpec) error {
	seenID := make(map[string]struct{})
	for _, st := range w.Steps {
		sid := strings.TrimSpace(st.ID)
		if sid != "" {
			if _, dup := seenID[sid]; dup {
				return fmt.Errorf("workflow %s: duplicate step id %q", wfName, sid)
			}
			seenID[sid] = struct{}{}
		}
		hasA := strings.TrimSpace(st.Agent) != ""
		hasU := strings.TrimSpace(st.Uses) != ""
		if hasA && hasU {
			return fmt.Errorf("workflow %s step %q: cannot set both agent and uses", wfName, sid)
		}
		if !hasA && !hasU {
			return fmt.Errorf("workflow %s step %q: must set exactly one of agent or uses", wfName, sid)
		}
		if hasU {
			u := strings.TrimSpace(st.Uses)
			if _, ok := spec.ParseToolUses(u); !ok {
				return fmt.Errorf("workflow %s step %q: unsupported uses %q (expected tool.<name>...)", wfName, sid, u)
			}
		}
	}
	return nil
}

func validateWorkflowStepOrder(wfName string, w *spec.WorkflowSpec) error {
	idToIdx := make(map[string]int)
	for i, st := range w.Steps {
		id := strings.TrimSpace(st.ID)
		if id == "" {
			continue
		}
		idToIdx[id] = i
	}
	for i, st := range w.Steps {
		sid := strings.TrimSpace(st.ID)
		for _, sval := range spec.CollectWithStringValues(st.With) {
			for _, dep := range spec.InterpolationStepRefs(sval) {
				j, ok := idToIdx[dep]
				if !ok {
					return fmt.Errorf("workflow %s step %q: interpolation references unknown step %q", wfName, sid, dep)
				}
				if j >= i {
					return fmt.Errorf("workflow %s step %q: forward reference to steps.%s (§9.4)", wfName, sid, dep)
				}
			}
		}
	}
	return nil
}
