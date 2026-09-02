package claudecode

import (
	"context"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/spec"
)

func TestMapLimits(t *testing.T) {
	// Nil constraints → default turns, no timeout, no budget.
	if l := MapLimits(nil, nil); l.MaxTurns != spec.DefaultAgentMaxIterations || l.Timeout != 0 || l.BudgetUSD != 0 {
		t.Fatalf("nil mapping = %+v", l)
	}
	// Explicit constraints map through; maxIterations shares the default-8/cap-32 clamp.
	c := &spec.AgentConstraints{MaxIterations: 99, TimeoutSeconds: 45}
	ex := &spec.PolicyExecution{MaxTotalCostUsd: 2.5}
	l := MapLimits(c, ex)
	if l.MaxTurns != spec.HardAgentMaxIterations {
		t.Fatalf("maxIterations 99 should clamp to %d, got %d", spec.HardAgentMaxIterations, l.MaxTurns)
	}
	if l.Timeout != 45*time.Second {
		t.Fatalf("timeout = %s", l.Timeout)
	}
	if l.BudgetUSD != 2.5 {
		t.Fatalf("budget = %v", l.BudgetUSD)
	}
}

func TestLimitsApplyTo(t *testing.T) {
	var rs RunSpec
	Limits{MaxTurns: 12, BudgetUSD: 3.0}.ApplyTo(&rs)
	if rs.MaxTurns != 12 || rs.MaxBudgetUSD != 3.0 {
		t.Fatalf("applied spec = %+v", rs)
	}
	// The mapped budget reaches the argv as --max-budget-usd.
	argv := ClaudeCodeRuntime{Bin: "claude"}.argv(rs)
	if !containsPair(argv, "--max-budget-usd", "3") || !containsPair(argv, "--max-turns", "12") {
		t.Fatalf("argv missing mapped limits: %v", argv)
	}
}

func TestLimitsWithTimeout(t *testing.T) {
	// No timeout → same ctx, deadline-less.
	base := context.Background()
	ctx, cancel := Limits{}.WithTimeout(base)
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("no timeout should leave the context deadline-less")
	}
	// Timeout → a deadline is set.
	ctx2, cancel2 := Limits{Timeout: 50 * time.Millisecond}.WithTimeout(base)
	defer cancel2()
	if _, ok := ctx2.Deadline(); !ok {
		t.Fatal("a timeout limit must set a context deadline")
	}
}

func budgetEvaluator(ceilingUSD float64) policy.PolicyEvaluator {
	return policy.NewEvaluator(&spec.ProjectGraph{}, &spec.PolicySpec{
		Execution: &spec.PolicyExecution{MaxTotalCostUsd: ceilingUSD},
	})
}

func TestEnforceBudget_WithinBudget(t *testing.T) {
	eval := budgetEvaluator(5.0)
	run := policy.RunContext{AccumulatedCostUSD: 1.0}
	got, err := EnforceBudget(context.Background(), eval, run, Session{CostUSD: 2.0})
	if err != nil {
		t.Fatalf("within budget must not error: %v", err)
	}
	if got.AccumulatedCostUSD != 3.0 {
		t.Fatalf("cost must be folded, got %v", got.AccumulatedCostUSD)
	}
}

// "Done when": a run that would exceed the Terfyn budget fails closed with a max_cost limit_hit,
// even though the harness's own --max-budget-usd accounting (the belt) let it finish.
func TestEnforceBudget_ExceedsBudget_FailsClosed(t *testing.T) {
	eval := budgetEvaluator(5.0)
	run := policy.RunContext{AccumulatedCostUSD: 4.0}
	_, err := EnforceBudget(context.Background(), eval, run, Session{CostUSD: 2.0}) // 6.0 > 5.0
	d, ok := policy.AsDenied(err)
	if !ok {
		t.Fatalf("over-budget run must fail closed with a denial, got %v", err)
	}
	if d.Reason != policy.ReasonMaxCost {
		t.Fatalf("reason = %q, want %q", d.Reason, policy.ReasonMaxCost)
	}
}

func TestEnforceBudget_NilEvalFoldsOnly(t *testing.T) {
	run := policy.RunContext{AccumulatedCostUSD: 1.0}
	got, err := EnforceBudget(context.Background(), nil, run, Session{CostUSD: 2.0})
	if err != nil {
		t.Fatalf("nil eval = no budget, got %v", err)
	}
	if got.AccumulatedCostUSD != 3.0 {
		t.Fatalf("cost must still be folded, got %v", got.AccumulatedCostUSD)
	}
}
