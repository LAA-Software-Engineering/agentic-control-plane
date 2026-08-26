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
}
