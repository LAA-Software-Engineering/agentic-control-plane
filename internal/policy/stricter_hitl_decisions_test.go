package policy

import (
	"testing"

	"github.com/Terfyn/terfyn/internal/spec"
)

func hitlDecisionsPolicy(decisions ...spec.HitlDecisionKind) *spec.PolicySpec {
	return &spec.PolicySpec{
		Hitl: &spec.HitlPolicy{
			InterruptOn: map[string]spec.HitlInterruptValue{
				"deploy": {Enabled: true, Config: &spec.HitlInterruptConfig{AllowedDecisions: decisions}},
			},
		},
	}
}

func mergedHitlDecisions(t *testing.T, a, b *spec.PolicySpec) []spec.HitlDecisionKind {
	t.Helper()
	merged := StricterOf(NewEvaluator(nil, a), NewEvaluator(nil, b)).(interface {
		PolicySpec() *spec.PolicySpec
	}).PolicySpec()
	review, err := ResolveHitlReview(nil, merged, "tool.deploy.run")
	if err != nil {
		t.Fatal(err)
	}
	return review.AllowedDecisions
}

// The reported bug: disjoint restrictions ({approve} vs {reject}) must permit NEITHER, not restore
// the default set (issue #357).
func TestStricterHitl_DisjointDecisionsFailClosed(t *testing.T) {
	for _, order := range []struct {
		name string
		a, b *spec.PolicySpec
	}{
		{"approve_then_reject", hitlDecisionsPolicy(spec.HitlDecisionApprove), hitlDecisionsPolicy(spec.HitlDecisionReject)},
		{"reject_then_approve", hitlDecisionsPolicy(spec.HitlDecisionReject), hitlDecisionsPolicy(spec.HitlDecisionApprove)},
	} {
		t.Run(order.name, func(t *testing.T) {
			got := mergedHitlDecisions(t, order.a, order.b)
			if IsDecisionAllowed(spec.HitlDecisionApprove, got) || IsDecisionAllowed(spec.HitlDecisionReject, got) {
				t.Fatalf("disjoint merge must permit neither decision, got %v", got)
			}
			if len(got) != 0 {
				t.Fatalf("deny-all must resolve to an empty decision set, got %v", got)
			}
		})
	}
}

// The stricter merge of overlapping restrictions is the intersection — the overlap stays allowed,
// the non-overlap is removed.
func TestStricterHitl_OverlappingDecisionsIntersect(t *testing.T) {
	a := hitlDecisionsPolicy(spec.HitlDecisionApprove, spec.HitlDecisionReject)
	b := hitlDecisionsPolicy(spec.HitlDecisionReject, spec.HitlDecisionEdit)
	got := mergedHitlDecisions(t, a, b)
	if !IsDecisionAllowed(spec.HitlDecisionReject, got) {
		t.Fatalf("the overlapping decision (reject) must remain allowed, got %v", got)
	}
	if IsDecisionAllowed(spec.HitlDecisionApprove, got) || IsDecisionAllowed(spec.HitlDecisionEdit, got) {
		t.Fatalf("non-overlapping decisions must be removed, got %v", got)
	}
}

// A side that leaves allowedDecisions unset means "defaults"; the stricter of defaults and an
// explicit set is that explicit set — not deny-all, and not the defaults.
func TestStricterHitl_UnsetSideKeepsExplicitRestriction(t *testing.T) {
	restricted := hitlDecisionsPolicy(spec.HitlDecisionApprove)
	unset := hitlDecisionsPolicy() // no decisions => defaults
	for _, tc := range []struct {
		name string
		a, b *spec.PolicySpec
	}{
		{"restricted_then_unset", restricted, unset},
		{"unset_then_restricted", unset, restricted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mergedHitlDecisions(t, tc.a, tc.b)
			if !IsDecisionAllowed(spec.HitlDecisionApprove, got) {
				t.Fatalf("approve must remain allowed, got %v", got)
			}
			if IsDecisionAllowed(spec.HitlDecisionReject, got) {
				t.Fatalf("reject was restricted away by the explicit side, got %v", got)
			}
		})
	}
}

// The unset-identity break (issue #357 review): an *unset* side resolves to its defaults
// ({approve, reject}, no edit), which is a proper subset of the universe. Merging it with an
// explicit {edit}-only side must NOT permit edit — the unset side never permitted it.
func TestStricterHitl_UnsetSideExcludesUnpermittedDecision(t *testing.T) {
	unset := hitlDecisionsPolicy()                         // defaults => {approve, reject}, no edit
	editOnly := hitlDecisionsPolicy(spec.HitlDecisionEdit) // explicit {edit}
	for _, tc := range []struct {
		name string
		a, b *spec.PolicySpec
	}{
		{"unset_then_edit", unset, editOnly},
		{"edit_then_unset", editOnly, unset},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mergedHitlDecisions(t, tc.a, tc.b)
			if IsDecisionAllowed(spec.HitlDecisionEdit, got) {
				t.Fatalf("edit must not survive: the unset side never permitted it, got %v", got)
			}
			// The two effective sets are disjoint ({approve,reject} vs {edit}), so nothing is permitted.
			if len(got) != 0 {
				t.Fatalf("stricter of disjoint effective sets must permit nothing, got %v", got)
			}
		})
	}
}

// Overlap through the unset side's defaults: unset ({approve,reject}) ∩ {approve,edit} = {approve}.
// approve survives (both permit it), reject is removed by the explicit side, edit is removed by the
// unset side's defaults.
func TestStricterHitl_UnsetIntersectsThroughDefaults(t *testing.T) {
	got := mergedHitlDecisions(t, hitlDecisionsPolicy(), hitlDecisionsPolicy(spec.HitlDecisionApprove, spec.HitlDecisionEdit))
	if !IsDecisionAllowed(spec.HitlDecisionApprove, got) {
		t.Fatalf("approve must survive (both sides permit it), got %v", got)
	}
	if IsDecisionAllowed(spec.HitlDecisionReject, got) {
		t.Fatalf("reject was restricted away by the explicit side, got %v", got)
	}
	if IsDecisionAllowed(spec.HitlDecisionEdit, got) {
		t.Fatalf("edit is not in the unset side's defaults, got %v", got)
	}
}

// An unset config still resolves to the default decision set (the sentinel path must not leak into
// the ordinary case).
func TestResolveHitl_UnsetResolvesToDefaults(t *testing.T) {
	review, err := ResolveHitlReview(nil, hitlDecisionsPolicy(), "tool.deploy.run")
	if err != nil {
		t.Fatal(err)
	}
	if !IsDecisionAllowed(spec.HitlDecisionApprove, review.AllowedDecisions) ||
		!IsDecisionAllowed(spec.HitlDecisionReject, review.AllowedDecisions) {
		t.Fatalf("an unset config must resolve to the default decisions, got %v", review.AllowedDecisions)
	}
}
