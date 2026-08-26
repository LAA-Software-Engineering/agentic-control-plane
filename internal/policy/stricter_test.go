package policy

import (
	"context"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestStricterOf_checkRunUsesTighterBudget(t *testing.T) {
	t.Parallel()
	loose := NewEvaluator(nil, &spec.PolicySpec{
		Execution: &spec.PolicyExecution{MaxTotalCostUsd: 1.0},
	})
	tight := NewEvaluator(nil, &spec.PolicySpec{
		Execution: &spec.PolicyExecution{MaxTotalCostUsd: 0.01},
	})
	ev := StricterOf(loose, tight)
	err := ev.CheckRun(context.Background(), RunContext{AccumulatedCostUSD: 0.5})
	if err == nil {
		t.Fatal("expected tighter ceiling to deny")
	}
	type specCarrier interface {
		PolicySpec() *spec.PolicySpec
	}
	c, ok := ev.(specCarrier)
	if !ok {
		t.Fatal("StricterOf should expose PolicySpec")
	}
	ps := c.PolicySpec()
	if ps == nil || ps.Execution == nil || ps.Execution.MaxTotalCostUsd != 0.01 {
		t.Fatalf("PolicySpec merge %+v", ps)
	}
}

func TestStricterOf_nilPassthrough(t *testing.T) {
	t.Parallel()
	a := NewEvaluator(nil, &spec.PolicySpec{
		Execution: &spec.PolicyExecution{MaxTotalCostUsd: 2},
	})
	if StricterOf(nil, a) != a {
		t.Fatal("nil left")
	}
	if StricterOf(a, nil) != a {
		t.Fatal("nil right")
	}
}

func TestMergeStricterPolicySpec_unionsHitlInterruptOn(t *testing.T) {
	t.Parallel()
	a := &spec.PolicySpec{
		Hitl: &spec.HitlPolicy{InterruptOn: map[string]spec.HitlInterruptValue{
			"helper": {Enabled: true},
		}},
	}
	b := &spec.PolicySpec{
		Hitl: &spec.HitlPolicy{InterruptOn: map[string]spec.HitlInterruptValue{
			"github": {Enabled: true},
		}},
	}
	got := mergeStricterPolicySpec(a, b)
	if got.Hitl == nil || !got.Hitl.InterruptOn["helper"].Enabled || !got.Hitl.InterruptOn["github"].Enabled {
		t.Fatalf("interruptOn union %+v", got.Hitl)
	}
	got.Hitl.InterruptOn["github"] = spec.HitlInterruptValue{}
	if !b.Hitl.InterruptOn["github"].Enabled {
		t.Fatal("merge must not alias callee Hitl")
	}
}

func TestMergeStricterPolicySpec_unionsRedactKeys(t *testing.T) {
	t.Parallel()
	a := &spec.PolicySpec{
		Hitl: &spec.HitlPolicy{RedactKeys: []string{"password", "token"}},
	}
	b := &spec.PolicySpec{
		Hitl: &spec.HitlPolicy{RedactKeys: []string{"token", "secret"}},
	}
	got := mergeStricterPolicySpec(a, b)
	if got.Hitl == nil {
		t.Fatal("nil hitl")
	}
	want := map[string]struct{}{"password": {}, "token": {}, "secret": {}}
	if len(got.Hitl.RedactKeys) != 3 {
		t.Fatalf("redact union %v", got.Hitl.RedactKeys)
	}
	for _, k := range got.Hitl.RedactKeys {
		if _, ok := want[k]; !ok {
			t.Fatalf("unexpected redact key %q in %v", k, got.Hitl.RedactKeys)
		}
	}
	got.Hitl.RedactKeys[0] = "mutated"
	if a.Hitl.RedactKeys[0] == "mutated" || b.Hitl.RedactKeys[0] == "mutated" {
		t.Fatal("redact merge must copy slices")
	}
}

func TestMergeStricterPolicySpec_intersectsSameKeyConfig(t *testing.T) {
	t.Parallel()
	a := &spec.PolicySpec{
		Hitl: &spec.HitlPolicy{
			InterruptOn: map[string]spec.HitlInterruptValue{
				"helper": {Enabled: true, Config: &spec.HitlInterruptConfig{
					AllowedDecisions: []spec.HitlDecisionKind{spec.HitlDecisionApprove, spec.HitlDecisionReject, spec.HitlDecisionEdit},
					AllowedEditArgs:  []string{"foo", "bar"},
					DeniedEditArgs:   []string{"secret"},
					RedactKeys:       []string{"token"},
				}},
			},
		},
	}
	b := &spec.PolicySpec{
		Hitl: &spec.HitlPolicy{
			InterruptOn: map[string]spec.HitlInterruptValue{
				"helper": {Enabled: true, Config: &spec.HitlInterruptConfig{
					AllowedDecisions: []spec.HitlDecisionKind{spec.HitlDecisionApprove, spec.HitlDecisionEdit},
					AllowedEditArgs:  []string{"bar", "baz"},
					DeniedEditArgs:   []string{"password"},
					RedactKeys:       []string{"secret"},
				}},
			},
		},
	}
	got := mergeStricterPolicySpec(a, b)
	cfg := got.Hitl.InterruptOn["helper"].Config
	if cfg == nil {
		t.Fatal("expected merged config")
	}
	if len(cfg.AllowedDecisions) != 2 || cfg.AllowedDecisions[0] != spec.HitlDecisionApprove || cfg.AllowedDecisions[1] != spec.HitlDecisionEdit {
		t.Fatalf("allowed decisions intersect got %v", cfg.AllowedDecisions)
	}
	if len(cfg.AllowedEditArgs) != 1 || cfg.AllowedEditArgs[0] != "bar" {
		t.Fatalf("allowed edit args intersect got %v", cfg.AllowedEditArgs)
	}
	if len(cfg.DeniedEditArgs) != 2 {
		t.Fatalf("denied edit args union got %v", cfg.DeniedEditArgs)
	}
	if len(cfg.RedactKeys) != 2 {
		t.Fatalf("config redact union got %v", cfg.RedactKeys)
	}
	cfg.AllowedEditArgs[0] = "mutated"
	if a.Hitl.InterruptOn["helper"].Config.AllowedEditArgs[0] == "mutated" {
		t.Fatal("must not alias caller config")
	}
	if b.Hitl.InterruptOn["helper"].Config.AllowedEditArgs[0] == "mutated" {
		t.Fatal("must not alias callee config")
	}
}
