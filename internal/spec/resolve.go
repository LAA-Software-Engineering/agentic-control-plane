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
	Pos      Pos
}

func (e *MissingRefError) Error() string {
	if e == nil {
		return ""
	}
	msg := fmt.Sprintf("%s references missing %s", e.Referrer.String(), e.Missing.String())
	if loc := e.Pos.String(); loc != "" {
		return loc + ": " + msg
	}
	return msg
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
		ar := g.Agents[agentName]
		for _, tn := range tools {
			if _, ok := g.Tools[tn]; !ok {
				errs = append(errs, &MissingRefError{
					Referrer: ResourceID{Kind: KindAgent, Name: agentName},
					Missing:  ResourceID{Kind: KindTool, Name: tn},
					Pos:      toolGrantPos(ar, tn),
				})
			}
		}
	}
	for agentName, pol := range ix.AgentPolicies {
		if _, ok := g.Policies[pol]; !ok && !IsBuiltinPreset(pol) {
			ar := g.Agents[agentName]
			pos := Pos{}
			if ar != nil {
				pos = ar.Pos
			}
			errs = append(errs, &MissingRefError{
				Referrer: ResourceID{Kind: KindAgent, Name: agentName},
				Missing:  ResourceID{Kind: KindPolicy, Name: pol},
				Pos:      pos,
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
					Pos:      workflowAgentPos(wr, an),
				})
			}
		}
		for _, tn := range ix.WorkflowTools[wfName] {
			if _, ok := g.Tools[tn]; !ok {
				errs = append(errs, &MissingRefError{
					Referrer: ResourceID{Kind: KindWorkflow, Name: wfName},
					Missing:  ResourceID{Kind: KindTool, Name: tn},
					Pos:      workflowUsesPos(wr, tn),
				})
			}
		}
		if pol := ix.WorkflowPolicies[wfName]; pol != "" {
			if _, ok := g.Policies[pol]; !ok && !IsBuiltinPreset(pol) {
				errs = append(errs, &MissingRefError{
					Referrer: ResourceID{Kind: KindWorkflow, Name: wfName},
					Missing:  ResourceID{Kind: KindPolicy, Name: pol},
					Pos:      wr.Pos,
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
				errs = append(errs, st.Pos.Errorf("workflow %s: duplicate step id %q", wfName, sid))
				continue
			}
			seenID[sid] = struct{}{}
		}
		hasA := strings.TrimSpace(st.Agent) != ""
		hasU := strings.TrimSpace(st.Uses) != ""
		if hasA && hasU {
			errs = append(errs, st.Pos.Errorf("workflow %s step %q: cannot set both agent and uses", wfName, sid))
			continue
		}
		if !hasA && !hasU {
			errs = append(errs, st.Pos.Errorf("workflow %s step %q: must set exactly one of agent or uses", wfName, sid))
			continue
		}
		if hasU {
			u := strings.TrimSpace(st.Uses)
			if _, ok := ParseToolUses(u); !ok {
				pos := st.UsesPos
				if pos.IsZero() {
					pos = st.Pos
				}
				errs = append(errs, pos.Errorf("workflow %s step %q: unsupported uses %q (expected tool.<name>...)", wfName, sid, u))
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
					return st.Pos.Errorf("workflow %s step %q: interpolation references unknown step %q", wfName, sid, dep)
				}
				if j >= i {
					return st.Pos.Errorf("workflow %s step %q: forward reference to steps.%s (§9.4)", wfName, sid, dep)
				}
			}
		}
	}
	return nil
}

func toolGrantPos(ar *AgentResource, toolName string) Pos {
	if ar == nil {
		return Pos{}
	}
	for i, t := range ar.Spec.Tools {
		tn := strings.TrimSpace(t)
		if n, ok := ParseToolUses(tn); ok {
			tn = n
		}
		if tn == toolName && i < len(ar.Spec.ToolsPos) {
			return ar.Spec.ToolsPos[i]
		}
	}
	return ar.Pos
}

func workflowAgentPos(wr *WorkflowResource, agentName string) Pos {
	if wr == nil {
		return Pos{}
	}
	for _, st := range wr.Spec.Steps {
		if strings.TrimSpace(st.Agent) == agentName {
			if !st.AgentPos.IsZero() {
				return st.AgentPos
			}
			return st.Pos
		}
	}
	return wr.Pos
}

func workflowUsesPos(wr *WorkflowResource, toolName string) Pos {
	if wr == nil {
		return Pos{}
	}
	for _, st := range wr.Spec.Steps {
		u := strings.TrimSpace(st.Uses)
		if tn, ok := ParseToolUses(u); ok && tn == toolName {
			if !st.UsesPos.IsZero() {
				return st.UsesPos
			}
			return st.Pos
		}
	}
	return wr.Pos
}
