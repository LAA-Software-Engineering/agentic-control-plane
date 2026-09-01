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
	vocab := declaredEffectIdents(g)
	var vs []AssertViolation

	// An effect ident that is malformed or not part of the project's declared vocabulary is a
	// violation, never a silent pass: forbidEffect is a negative guarantee, so a typo'd
	// ("workspace.wirte") or nonexistent effect must fail loudly rather than hold vacuously.
	identViolation := func(kind, ident string) (AssertViolation, bool) {
		ident = strings.TrimSpace(ident)
		if err := spec.ValidateEffectIdent(ident); err != nil {
			return AssertViolation{kind, fmt.Sprintf("malformed effect ident %q: %v", ident, err)}, true
		}
		if !effectIdentRecognized(ident, vocab) {
			return AssertViolation{kind, fmt.Sprintf("effect %q is not declared by any tool operation in the project (typo? an assertion that names a nonexistent effect cannot be trusted)", ident)}, true
		}
		return AssertViolation{}, false
	}

	for _, re := range a.ForbidEffect {
		if v, bad := identViolation("forbidEffect", re.Effect); bad {
			vs = append(vs, v)
			continue
		}
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
		if v, bad := identViolation("expectAutonomous", re.Effect); bad {
			vs = append(vs, v)
			continue
		}
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

// effectByIdent finds a reachable effect that matches ident hierarchically, the same way permit
// resolution treats a namespace parent as covering its children (spec.EffectCovers). So a forbid on
// a parent ("workspace") matches a reachable child ("workspace.write"), and a forbid on a leaf
// matches a reachable broad parent — the guarantee cannot be dodged by naming a different level.
func effectByIdent(b Bound, ident string) (Effect, bool) {
	ident = strings.TrimSpace(ident)
	for _, e := range b.Effects {
		if e.Unknown {
			continue
		}
		if e.Ident == ident || spec.EffectCovers(ident, e.Ident) || spec.EffectCovers(e.Ident, ident) {
			return e, true
		}
	}
	return Effect{}, false
}

// declaredEffectIdents is the project's effect vocabulary: every effect ident declared on any tool
// operation. An assertion effect that neither equals nor is hierarchically related to one of these
// is unrecognized (a typo, or an effect no operation produces).
func declaredEffectIdents(g *spec.ProjectGraph) []string {
	seen := map[string]bool{}
	var out []string
	if g == nil {
		return out
	}
	for name, tr := range g.Tools {
		if tr == nil {
			continue
		}
		for _, effs := range spec.ResolveToolEffects(name, &tr.Spec).ByOperation {
			for _, e := range effs {
				if e = strings.TrimSpace(e); e != "" && !seen[e] {
					seen[e] = true
					out = append(out, e)
				}
			}
		}
	}
	return out
}

// effectIdentRecognized reports whether ident equals, covers, or is covered by a declared effect —
// so a leaf named against a broad-grant parent (and vice versa) is recognized, but a typo is not.
func effectIdentRecognized(ident string, vocab []string) bool {
	ident = strings.TrimSpace(ident)
	for _, d := range vocab {
		if d == ident || spec.EffectCovers(d, ident) || spec.EffectCovers(ident, d) {
			return true
		}
	}
	return false
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
