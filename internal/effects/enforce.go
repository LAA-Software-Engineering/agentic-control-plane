package effects

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
)

// Check compares [Compute] bounds against each root's Policy.spec.effects (issue #190).
// It is a static validate/plan check, not a runtime CheckToolCall change.
//
// If no Tool in g declares spec.operations effects, Check is a no-op so existing
// examples and fixtures still validate. Otherwise a policy with no permit /
// permitWithApproval block permits nothing; the error names the policy.
func Check(g *spec.ProjectGraph) error {
	if g == nil || !anyDeclaredToolEffects(g) {
		return nil
	}
	bounds := Compute(g)
	var errs []error
	usedAgents := workflowAgentNames(g)
	for _, name := range sortedKeys(g.Workflows) {
		b := bounds.Workflows[name]
		polName, pol := policyFor(g, g.Workflows[name])
		errs = append(errs, checkBound(g, b, polName, pol)...)
	}
	for _, name := range sortedKeys(g.Agents) {
		if _, used := usedAgents[name]; used {
			continue
		}
		b := bounds.Agents[name]
		polName, pol := policyForAgent(g, g.Agents[name])
		errs = append(errs, checkBound(g, b, polName, pol)...)
	}
	return errors.Join(errs...)
}

func anyDeclaredToolEffects(g *spec.ProjectGraph) bool {
	if g == nil {
		return false
	}
	for name, tr := range g.Tools {
		if tr == nil {
			continue
		}
		if !spec.ResolveToolEffects(name, &tr.Spec).Unknown {
			return true
		}
	}
	return false
}

func workflowAgentNames(g *spec.ProjectGraph) map[string]struct{} {
	out := map[string]struct{}{}
	if g == nil {
		return out
	}
	for _, wf := range g.Workflows {
		if wf == nil {
			continue
		}
		for _, st := range wf.Spec.Steps {
			if a := strings.TrimSpace(st.Agent); a != "" {
				out[a] = struct{}{}
			}
		}
	}
	return out
}

func policyFor(g *spec.ProjectGraph, wf *spec.WorkflowResource) (string, *spec.PolicySpec) {
	name := ""
	if wf != nil {
		name = strings.TrimSpace(wf.Spec.Policy)
	}
	return lookupPolicy(g, name)
}

func policyForAgent(g *spec.ProjectGraph, ar *spec.AgentResource) (string, *spec.PolicySpec) {
	name := ""
	if ar != nil {
		name = strings.TrimSpace(ar.Spec.Policy)
	}
	return lookupPolicy(g, name)
}

func lookupPolicy(g *spec.ProjectGraph, name string) (string, *spec.PolicySpec) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "(none)", nil
	}
	if g == nil || g.Policies == nil {
		return name, nil
	}
	pr, ok := g.Policies[name]
	if !ok || pr == nil {
		return name, nil
	}
	return name, &pr.Spec
}

func checkBound(g *spec.ProjectGraph, b Bound, policyName string, pol *spec.PolicySpec) []error {
	permit, withApproval := permitLists(pol)
	var errs []error
	for _, e := range b.Effects {
		if e.Unknown {
			errs = append(errs, &Violation{
				Policy:   policyName,
				RootKind: b.RootKind,
				RootName: b.RootName,
				Unknown:  true,
				Message:  e.Message,
				Uses:     e.Uses,
				Witness:  e.Witness,
				Permits:  displayPermits(permit, withApproval),
			})
			continue
		}
		inPermit := identCovered(permit, e.Ident)
		inApproval := identCovered(withApproval, e.Ident)
		if !inPermit && !inApproval {
			errs = append(errs, &Violation{
				Policy:   policyName,
				RootKind: b.RootKind,
				RootName: b.RootName,
				Ident:    e.Ident,
				Uses:     e.Uses,
				Witness:  e.Witness,
				Permits:  displayPermits(permit, withApproval),
			})
			continue
		}
		if inPermit && !inApproval && witnessingRequiresApproval(g, pol, e.Uses) {
			errs = append(errs, &Violation{
				Policy:      policyName,
				RootKind:    b.RootKind,
				RootName:    b.RootName,
				Ident:       e.Ident,
				Uses:        e.Uses,
				Witness:     e.Witness,
				Permits:     displayPermits(permit, withApproval),
				RuleApplied: "requiresApproval",
			})
		}
	}
	return errs
}

func permitLists(pol *spec.PolicySpec) (permit, withApproval []string) {
	if pol == nil || pol.Effects == nil {
		return nil, nil
	}
	return pol.Effects.Permit, pol.Effects.PermitWithApproval
}

func identCovered(list []string, ident string) bool {
	ident = strings.TrimSpace(ident)
	if ident == "" {
		return false
	}
	for _, p := range list {
		if spec.EffectCovers(p, ident) {
			return true
		}
	}
	return false
}

func displayPermits(permit, withApproval []string) string {
	var parts []string
	parts = append(parts, permit...)
	if len(withApproval) > 0 {
		parts = append(parts, "permitWithApproval: "+strings.Join(withApproval, ", "))
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, ", ")
}

func witnessingRequiresApproval(g *spec.ProjectGraph, pol *spec.PolicySpec, uses string) bool {
	if pol != nil && pol.Approvals != nil && spec.ApprovalPermissive(pol.Approvals) {
		return false
	}
	if pol != nil && spec.ApprovalRequireAllTools(pol.Approvals) {
		return true
	}
	if pol != nil && pol.Approvals != nil {
		u := strings.TrimSpace(uses)
		for _, r := range pol.Approvals.RequiredFor {
			if strings.TrimSpace(r) == u {
				return true
			}
		}
	}
	tn, _, err := tools.ParseUses(uses)
	if err != nil {
		return true
	}
	var safety *spec.ToolSafety
	if g != nil && g.Tools != nil {
		if tr := g.Tools[tn]; tr != nil {
			safety = tr.Spec.Safety
		}
	}
	return spec.ResolveToolSafety(safety).RequiresApproval
}

// Violation is one unpermitted (or approval-conflict) reachable effect.
type Violation struct {
	Policy      string
	RootKind    HopKind
	RootName    string
	Ident       string
	Unknown     bool
	Message     string
	Uses        string
	Witness     []Hop
	Permits     string
	RuleApplied string // "requiresApproval" when permit disagrees with requiredFor/ToolSafety
}

func (v *Violation) Error() string {
	if v == nil {
		return ""
	}
	var b strings.Builder
	if v.RuleApplied == "requiresApproval" {
		b.WriteString("Error: effect permit disagrees with requiresApproval\n\n")
		fmt.Fprintf(&b, "  %s effect `%s` is listed under Policy/%s spec.effects.permit (unattended)\n",
			rootLabel(v.RootKind, v.RootName), v.Ident, v.Policy)
		b.WriteString("  but ToolSafety/approvals.requiredFor requires approval; stricter rule applied (requiresApproval).\n\n")
	} else {
		b.WriteString("Error: effect not permitted by policy\n\n")
		if v.Unknown {
			fmt.Fprintf(&b, "  %s may perform an unknown effect\n", rootLabel(v.RootKind, v.RootName))
			if msg := strings.TrimSpace(v.Message); msg != "" {
				fmt.Fprintf(&b, "\n  %s\n", msg)
			}
		} else {
			fmt.Fprintf(&b, "  %s may perform effect `%s`\n", rootLabel(v.RootKind, v.RootName), v.Ident)
		}
		b.WriteByte('\n')
	}
	b.WriteString(formatWitness(v.Witness, v.Ident, v.Unknown, v.Uses))
	fmt.Fprintf(&b, "\n  Policy/%s permits: %s", v.Policy, v.Permits)
	return b.String()
}

func rootLabel(kind HopKind, name string) string {
	switch kind {
	case KindWorkflow:
		return "Workflow/" + name
	case KindAgent:
		return "Agent/" + name
	default:
		return string(kind) + "/" + name
	}
}

func formatWitness(hops []Hop, ident string, unknown bool, uses string) string {
	if len(hops) == 0 {
		return ""
	}
	auto := false
	for _, h := range hops {
		if h.Reachability == Autonomous {
			auto = true
		}
	}
	fx := ident
	if unknown || strings.TrimSpace(fx) == "" {
		fx = "unknown"
	}
	var b strings.Builder
	b.WriteString("  reachable via:\n")
	skipAgent := false
	for i, h := range hops {
		switch h.Kind {
		case KindWorkflow:
			fmt.Fprintf(&b, "    Workflow/%s\n", h.Name)
		case KindStep:
			stepName := h.Name
			if stepName == "" {
				stepName = h.ID
			}
			agentName := ""
			for _, n := range hops[i+1:] {
				if n.Kind == KindAgent {
					agentName = n.Name
					skipAgent = true
					break
				}
			}
			if agentName != "" {
				tag := "static"
				if auto {
					tag = "AUTONOMOUS"
				}
				fmt.Fprintf(&b, "      → step %s  (Agent/%s, %s)\n", stepName, agentName, tag)
			} else {
				fmt.Fprintf(&b, "      → step %s\n", stepName)
			}
		case KindAgent:
			if skipAgent {
				continue
			}
			tag := "static"
			if auto {
				tag = "AUTONOMOUS"
			}
			fmt.Fprintf(&b, "      → Agent/%s  (%s)\n", h.Name, tag)
		case KindToolOperation:
			name := h.Name
			if name == "" {
				name = uses
			}
			fmt.Fprintf(&b, "        → %s  [%s]\n", name, fx)
		}
	}
	return b.String()
}
