package claudecode

import (
	"context"
	"fmt"

	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/trace"
)

// Trace / audit integration for external-runtime runs (issue #341). A completed external session is
// folded into the run's hash-linked trace_events so `terfyn logs` and `terfyn audit verify` cover a
// `--runtime claude-code` run identically to an internal-loop run. The inner tool calls are already
// emitted by the mcpserver PolicyDispatcher (via its WithTrace); this file emits the agent turns and
// the budget limit_hit, using the same shared trace builders, so the chain is indistinguishable in
// structure from a local run.

// EmitSessionTurns records one llm_completion event per assistant turn in a completed session. The
// session's total cost is attributed to the final turn (the result event carries the run total),
// matching how a local run accrues cost across Generate turns. Emission is best-effort: a
// trace-store error is returned but does not need to fail the run. A nil recorder is a no-op.
func EmitSessionTurns(ctx context.Context, rec *trace.Recorder, runID, agent string, session Session) error {
	if rec == nil || runID == "" {
		return nil
	}
	for i := range session.Turns {
		cost := 0.0
		if i == len(session.Turns)-1 {
			cost = session.CostUSD // total run cost lands on the terminal turn
		}
		stepID := fmt.Sprintf("turn-%d", i+1)
		if _, err := rec.Append(ctx, runID, stepID, trace.EventLLMCompletion, trace.ActorAgent,
			trace.LLMCompletionData(agent, session.Model, cost)); err != nil {
			return fmt.Errorf("claudecode: emit turn trace: %w", err)
		}
	}
	return nil
}

// EmitLimitHit records a limit_hit event for a budget breach returned by EnforceBudget (#340). It
// mirrors the internal loop's max_cost limit_hit (same payload via trace.LimitHitData), so an
// external run that fails closed on budget is auditable the same way. Only a max_cost / max_wall_clock
// denial is recorded; any other error is ignored (the caller surfaces it separately). A nil recorder
// is a no-op.
func EmitLimitHit(ctx context.Context, rec *trace.Recorder, runID, stepID string, err error) error {
	if rec == nil || runID == "" {
		return nil
	}
	d, ok := policy.AsDenied(err)
	if !ok {
		return nil
	}
	var kind string
	switch d.Reason {
	case policy.ReasonMaxCost:
		kind = "max_cost"
	case policy.ReasonMaxWallClock:
		kind = "max_wall_clock"
	default:
		return nil
	}
	if _, aerr := rec.Append(ctx, runID, stepID, trace.EventLimitHit, trace.ActorSystem,
		trace.LimitHitData(kind, stepID, d.Extra)); aerr != nil {
		return fmt.Errorf("claudecode: emit limit_hit trace: %w", aerr)
	}
	return nil
}
