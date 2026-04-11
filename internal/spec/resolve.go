package spec

import (
	"errors"
	"fmt"
	"strings"
)

// MissingRefError reports a reference from Referrer to a missing resource (§9.1).
type MissingRefError struct {
	Referrer ResourceID
	Missing  ResourceID
}

func (e *MissingRefError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s references missing %s", e.Referrer.String(), e.Missing.String())
}

// ResolveReferences checks symbolic references and workflow step rules (§9.4).
// Multiple problems are combined with [errors.Join].
func ResolveReferences(g *ProjectGraph) error {
	return errors.Join(collectReferenceErrors(g)...)
}

func collectReferenceErrors(g *ProjectGraph) []error {
	if g == nil {
		return nil
	}
	var errs []error
	ix := BuildRefIndex(g)

	for agentName, tools := range ix.AgentTools {
		for _, tn := range tools {
			if _, ok := g.Tools[tn]; !ok {
				errs = append(errs, &MissingRefError{
					Referrer: ResourceID{Kind: KindAgent, Name: agentName},
					Missing:  ResourceID{Kind: KindTool, Name: tn},
				})
			}
		}
	}
	for agentName, pol := range ix.AgentPolicies {
		if _, ok := g.Policies[pol]; !ok {
			errs = append(errs, &MissingRefError{
				Referrer: ResourceID{Kind: KindAgent, Name: agentName},
				Missing:  ResourceID{Kind: KindPolicy, Name: pol},
			})
		}
	}

	for wfName, wr := range g.Workflows {
		if wr == nil {
			continue
		}
		errs = append(errs, validateWorkflowStepErrors(wfName, &wr.Spec)...)
		for _, an := range ix.WorkflowAgents[wfName] {
			if _, ok := g.Agents[an]; !ok {
				errs = append(errs, &MissingRefError{
					Referrer: ResourceID{Kind: KindWorkflow, Name: wfName},
					Missing:  ResourceID{Kind: KindAgent, Name: an},
				})
			}
		}
		for _, tn := range ix.WorkflowTools[wfName] {
			if _, ok := g.Tools[tn]; !ok {
				errs = append(errs, &MissingRefError{
					Referrer: ResourceID{Kind: KindWorkflow, Name: wfName},
					Missing:  ResourceID{Kind: KindTool, Name: tn},
				})
			}
		}
		if pol := ix.WorkflowPolicies[wfName]; pol != "" {
			if _, ok := g.Policies[pol]; !ok {
				errs = append(errs, &MissingRefError{
					Referrer: ResourceID{Kind: KindWorkflow, Name: wfName},
					Missing:  ResourceID{Kind: KindPolicy, Name: pol},
				})
			}
		}
		if e := validateWorkflowStepOrder(wfName, &wr.Spec); e != nil {
			errs = append(errs, e)
		}
	}
	return errs
}

func validateWorkflowStepErrors(wfName string, w *WorkflowSpec) []error {
	var errs []error
	seenID := make(map[string]struct{})
	for _, st := range w.Steps {
		sid := strings.TrimSpace(st.ID)
		if sid != "" {
			if _, dup := seenID[sid]; dup {
				errs = append(errs, fmt.Errorf("workflow %s: duplicate step id %q", wfName, sid))
				continue
			}
			seenID[sid] = struct{}{}
		}
		hasA := strings.TrimSpace(st.Agent) != ""
		hasU := strings.TrimSpace(st.Uses) != ""
		if hasA && hasU {
			errs = append(errs, fmt.Errorf("workflow %s step %q: cannot set both agent and uses", wfName, sid))
			continue
		}
		if !hasA && !hasU {
			errs = append(errs, fmt.Errorf("workflow %s step %q: must set exactly one of agent or uses", wfName, sid))
			continue
		}
		if hasU {
			u := strings.TrimSpace(st.Uses)
			if _, ok := ParseToolUses(u); !ok {
				errs = append(errs, fmt.Errorf("workflow %s step %q: unsupported uses %q (expected tool.<name>...)", wfName, sid, u))
			}
		}
	}
	return errs
}

func validateWorkflowStepOrder(wfName string, w *WorkflowSpec) error {
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
		for _, sval := range CollectWithStringValues(st.With) {
			for _, dep := range InterpolationStepRefs(sval) {
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
