package engine

import (
	"context"
	"time"

	"github.com/Terfyn/terfyn/internal/policy"
)

// withSecondsTimeout returns a child context with timeout when seconds > 0; otherwise parent and a no-op cancel.
func withSecondsTimeout(parent context.Context, seconds int) (context.Context, context.CancelFunc) {
	if seconds <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, time.Duration(seconds)*time.Second)
}

// wallClockDeadline bounds ctx by the run's REMAINING wall-clock budget
// (maxWallClockSeconds minus elapsed), so a hung tool or model call cannot outlive
// the declared bound. CheckRun only evaluates the budget BETWEEN steps, so an
// undeadlined transport (a server that accepts a connection and never answers)
// blocks the run forever despite maxWallClockSeconds (#394). Returns the parent and
// a no-op cancel when no positive bound applies or the run-start reference is unset.
func (e *Executor) wallClockDeadline(ctx context.Context, pol policy.PolicyEvaluator, pctx policy.RunContext) (context.Context, context.CancelFunc) {
	ps := policySpecFromEvaluator(pol)
	if ps == nil || ps.Execution == nil || ps.Execution.MaxWallClockSeconds <= 0 || pctx.StartedAt.IsZero() {
		return ctx, func() {}
	}
	limit := time.Duration(ps.Execution.MaxWallClockSeconds) * time.Second
	remaining := limit - e.now().Sub(pctx.StartedAt)
	if remaining < 0 {
		remaining = 0 // already over budget — fire immediately rather than grant a fresh full window
	}
	return context.WithTimeout(ctx, remaining)
}
