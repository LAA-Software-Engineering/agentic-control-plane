package engine

import (
	"context"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/tools"
)

// TestRunToolStep_wallClockBoundsHangingTool proves a `uses:` tool step whose transport hangs is
// cancelled when the run's maxWallClockSeconds budget expires, instead of blocking the run forever —
// CheckRun only evaluates the budget between steps, so without a per-call deadline a hung server
// never trips it (#394).
func TestRunToolStep_wallClockBoundsHangingTool(t *testing.T) {
	t.Parallel()
	graph := &spec.ProjectGraph{Tools: map[string]*spec.ToolResource{
		"slow": {Metadata: spec.Metadata{Name: "slow"}, Spec: spec.ToolSpec{
			Type:   "mock",
			Safety: &spec.ToolSafety{Trusted: spec.BoolPtr(true), SideEffects: spec.BoolPtr(false), RequiresApproval: spec.BoolPtr(false)},
		}},
	}}
	exec := &tools.MockExecutor{Fn: func(ctx context.Context, _ tools.ToolCallRequest) (tools.ToolCallResponse, error) {
		<-ctx.Done() // hang until the context is cancelled
		return tools.ToolCallResponse{}, ctx.Err()
	}}
	e := &Executor{Graph: graph, Tools: exec}
	pol := policy.NewEvaluator(graph, &spec.PolicySpec{Execution: &spec.PolicyExecution{MaxWallClockSeconds: 1}})
	pctx := policy.RunContext{StartedAt: time.Now()}

	done := make(chan error, 1)
	go func() {
		_, _, err := e.runToolStep(context.Background(), nil, pol, nil, "run-1",
			spec.WorkflowStep{ID: "s1", Uses: "tool.slow.ping"}, map[string]any{}, pctx, "", nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected the hung tool call to be cancelled by the wall-clock budget")
		}
	case <-time.After(6 * time.Second):
		t.Fatal("runToolStep hung well past the 1s wall-clock budget: no per-call deadline applied")
	}
}

// TestWallClockDeadline_noBoundIsUnbounded proves that without a positive maxWallClockSeconds (or
// without a run-start reference) the call context is left unbounded, so a legitimate long call is
// not spuriously cancelled.
func TestWallClockDeadline_noBoundIsUnbounded(t *testing.T) {
	t.Parallel()
	e := &Executor{}
	// No policy execution → no bound.
	pol := policy.NewEvaluator(nil, nil)
	ctx, cancel := e.wallClockDeadline(context.Background(), pol, policy.RunContext{StartedAt: time.Now()})
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("expected no deadline when maxWallClockSeconds is unset")
	}

	// A positive bound but no StartedAt reference → also unbounded (cannot compute remaining safely).
	pol2 := policy.NewEvaluator(nil, &spec.PolicySpec{Execution: &spec.PolicyExecution{MaxWallClockSeconds: 30}})
	ctx2, cancel2 := e.wallClockDeadline(context.Background(), pol2, policy.RunContext{})
	defer cancel2()
	if _, ok := ctx2.Deadline(); ok {
		t.Fatal("expected no deadline when the run-start reference is unset")
	}
}
