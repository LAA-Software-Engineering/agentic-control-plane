package effects

import (
	"fmt"
	"strings"

	"github.com/Terfyn/terfyn/internal/spec"
)

// Declarative, model-free capability assertions over the effect bound (issue #332). The central
// security property of a bounded agent system is a capability invariant — "the Reviewer can never
// reach workspace.write", "the Implementer autonomously may", "these publish ops are always gated".
// terfyn plan already computes the bound; these assertions turn an invariant into a checked-in,
// CI-enforceable statement evaluated statically (no model, no run), so the guarantee lives next to
// the agents it constrains.

// RootEffect names a bound root (an agent or workflow metadata name) and an effect ident.
type RootEffect struct {
	Root   string
	Effect string
}

// CapabilityAssertions is a set of invariants to check against a graph's effect bound.
type CapabilityAssertions struct {
	// ForbidEffect: the root must NOT be able to reach the effect (any reachability).
	ForbidEffect []RootEffect
	// ExpectAutonomous: the root must reach the effect via an autonomous (agent tool-selection) path.
	ExpectAutonomous []RootEffect
	// ExpectGated: each tool.<name>.<op> must require approval from every root that can reach it.
	ExpectGated []string
}

// Empty reports whether there is nothing to check.
func (a CapabilityAssertions) Empty() bool {
	return len(a.ForbidEffect)+len(a.ExpectAutonomous)+len(a.ExpectGated) == 0
}

// Violation is one failed assertion, with the assertion kind and a human-readable reason.
type AssertViolation struct {
	Kind   string // "forbidEffect" | "expectAutonomous" | "expectGated"
	Detail string
}

// Evaluate computes the effect bound for g and returns every assertion violation (nil when all
// invariants hold).
func (a CapabilityAssertions) Evaluate(g *spec.ProjectGraph) []AssertViolation {
	bounds := Compute(g)
	var vs []AssertViolation

	for _, re := range a.ForbidEffect {
		b, ok := boundForRoot(bounds, re.Root)
		if !ok {
			vs = append(vs, AssertViolation{"forbidEffect", fmt.Sprintf("root %q not found (no agent or workflow by that name)", re.Root)})
			continue
		}
		if e, found := effectByIdent(b, re.Effect); found {
			vs = append(vs, AssertViolation{"forbidEffect", fmt.Sprintf("%s %q can reach %q (%s) but is forbidden from it", b.RootKind, re.Root, re.Effect, witnessString(e))})
		}
	}

	for _, re := range a.ExpectAutonomous {
		b, ok := boundForRoot(bounds, re.Root)
		if !ok {
			vs = append(vs, AssertViolation{"expectAutonomous", fmt.Sprintf("root %q not found (no agent or workflow by that name)", re.Root)})
			continue
		}
		e, found := effectByIdent(b, re.Effect)
		if !found {
			vs = append(vs, AssertViolation{"expectAutonomous", fmt.Sprintf("%s %q cannot reach %q at all", b.RootKind, re.Root, re.Effect)})
			continue
		}
		if !hasAutonomousWitness(e) {
			vs = append(vs, AssertViolation{"expectAutonomous", fmt.Sprintf("%s %q reaches %q only statically, not autonomously", b.RootKind, re.Root, re.Effect)})
		}
	}

	for _, uses := range a.ExpectGated {
		uses = strings.TrimSpace(uses)
		reachable, ungatedRoot := gatingStatus(g, bounds, uses)
		switch {
		case !reachable:
			vs = append(vs, AssertViolation{"expectGated", fmt.Sprintf("%q is not reachable from any root; cannot assert it is gated", uses)})
		case ungatedRoot != "":
			vs = append(vs, AssertViolation{"expectGated", fmt.Sprintf("%q is reachable from %q but that root's policy does not require approval for it", uses, ungatedRoot)})
		}
	}

	return vs
}

func boundForRoot(gb GraphBounds, name string) (Bound, bool) {
	name = strings.TrimSpace(name)
	if b, ok := gb.Agents[name]; ok {
		return b, true
	}
	if b, ok := gb.Workflows[name]; ok {
		return b, true
	}
	return Bound{}, false
}

func effectByIdent(b Bound, ident string) (Effect, bool) {
	ident = strings.TrimSpace(ident)
	for _, e := range b.Effects {
		if !e.Unknown && e.Ident == ident {
			return e, true
		}
	}
	return Effect{}, false
}

func hasAutonomousWitness(e Effect) bool {
	if witnessIsAutonomous(e.Witness) {
		return true
	}
	for _, occ := range e.occurrences {
		if witnessIsAutonomous(occ.witness) {
			return true
		}
	}
	return false
}

func witnessIsAutonomous(hops []Hop) bool {
	for _, h := range hops {
		if h.Reachability == Autonomous {
			return true
		}
	}
	return false
}

func witnessString(e Effect) string {
	if u := strings.TrimSpace(e.Uses); u != "" {
		return u
	}
	for _, h := range e.Witness {
		if h.Kind == KindToolOperation && strings.TrimSpace(h.Name) != "" {
			return h.Name
		}
	}
	return "reachable"
}

// gatingStatus reports whether uses is reachable from any root and, if so, the name of the first
// root whose governing policy does NOT require approval for it (empty when every reaching root gates
// it). It uses the same per-root policy resolution and approval check as effect-bound enforcement.
func gatingStatus(g *spec.ProjectGraph, bounds GraphBounds, uses string) (reachable bool, ungatedRoot string) {
	visit := func(rootName string, b Bound, pol *spec.PolicySpec) {
		for _, e := range b.Effects {
			for _, occ := range occurrencesOf(e) {
				if strings.TrimSpace(occ.uses) != uses {
					continue
				}
				reachable = true
				if ungatedRoot == "" && !witnessingRequiresApproval(g, pol, uses) {
					ungatedRoot = rootName
				}
			}
		}
	}
	for _, name := range sortedKeys(g.Workflows) {
		_, pol := policyFor(g, g.Workflows[name])
		visit(name, bounds.Workflows[name], pol)
	}
	used := workflowAgentNames(g)
	for _, name := range sortedKeys(g.Agents) {
		if _, isUsed := used[name]; isUsed {
			continue
		}
		_, pol := policyForAgent(g, g.Agents[name])
		visit(name, bounds.Agents[name], pol)
	}
	return reachable, ungatedRoot
}

func occurrencesOf(e Effect) []effectOccurrence {
	if len(e.occurrences) > 0 {
		return e.occurrences
	}
	if strings.TrimSpace(e.Uses) != "" {
		return []effectOccurrence{{uses: e.Uses, witness: e.Witness}}
	}
	return nil
}
