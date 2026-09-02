package mcpserver

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/tools"
	"github.com/Terfyn/terfyn/internal/trace"
)

// Dispatcher runs one granted MCP tool call. The Server has already enforced the closed
// world (the uses string corresponds to an advertised grant); the Dispatcher's job is to run
// that call through the rest of Terfyn's authority path and return the tool output.
type Dispatcher interface {
	Call(ctx context.Context, uses string, args map[string]any) (map[string]any, error)
}

// PolicyDispatcher routes each call through the same inner path as Terfyn's own engine loop:
// policy CheckToolCall (which enforces the closed-world manifest, forbidUnknownTools, and
// approvals) and then tools.ToolExecutor.Call. It holds the run-level context so budget and
// approval decisions see the live run state.
//
// Interactive HITL prompting for an out-of-process agent is not resolved here: CheckToolCall
// enforces approvals as policy (an unapproved required action is denied, surfaced to the agent
// as an isError result), and wiring an interactive approval round-trip through the external
// runtime is left to the live runtime integration (#341).
type PolicyDispatcher struct {
	eval  policy.PolicyEvaluator
	exec  tools.ToolExecutor
	run   policy.RunContext
	seq   atomic.Int64
	rec   *trace.Recorder
	runID string
}

// NewPolicyDispatcher builds a dispatcher bound to a policy evaluator, a tool executor, and
// the run context that approval/budget checks read.
func NewPolicyDispatcher(eval policy.PolicyEvaluator, exec tools.ToolExecutor, run policy.RunContext) *PolicyDispatcher {
	return &PolicyDispatcher{eval: eval, exec: exec, run: run}
}

// WithTrace makes the dispatcher fold each external tool call into the run's hash-linked trace
// (issue #341): a tool_selection before the policy check and a tool_execution after the attempt,
// with the same payload shape as the internal loop, so the audit chain is indistinguishable from a
// local run. A nil recorder or empty runID disables emission. Returns the dispatcher for chaining.
func (d *PolicyDispatcher) WithTrace(rec *trace.Recorder, runID string) *PolicyDispatcher {
	d.rec = rec
	d.runID = runID
	return d
}

// Call enforces policy then executes. A policy denial or executor error is returned as-is so
// the Server can surface it to the agent while Terfyn's trace records the denial.
func (d *PolicyDispatcher) Call(ctx context.Context, uses string, args map[string]any) (map[string]any, error) {
	if d.eval == nil || d.exec == nil {
		return nil, fmt.Errorf("mcpserver: dispatcher not fully configured")
	}
	stepID := fmt.Sprintf("mcp-%d", d.seq.Add(1))
	toolName, _, _ := tools.ParseUses(uses)

	if err := d.eval.CheckToolCall(ctx, policy.ToolCallContext{
		Run:    d.run,
		StepID: stepID,
		Uses:   uses,
		With:   args,
	}); err != nil {
		// Fail-closed, exactly as the internal loop: the tool is never invoked, so we skip
		// tool_selection/tool_execution and record a system_error instead — keeping the external
		// run's chain structurally identical to a local run's on a denial.
		d.traceDenied(ctx, stepID, err)
		return nil, err
	}
	d.traceSelection(ctx, stepID, uses, toolName, args)
	resp, err := d.exec.Call(ctx, tools.ToolCallRequest{Uses: uses, With: args})
	d.traceExecution(ctx, stepID, uses, toolName, resp.Meta, err)
	if err != nil {
		return nil, err
	}
	return resp.Output, nil
}

// traceSelection / traceExecution / traceDenied emit the inner-call events when a recorder is
// configured. Like the engine, emission is best-effort: a trace-store error never fails the call.
func (d *PolicyDispatcher) traceSelection(ctx context.Context, stepID, uses, toolName string, args map[string]any) {
	if d.rec == nil || d.runID == "" {
		return
	}
	_, _ = d.rec.Append(ctx, d.runID, stepID, trace.EventToolSelection, trace.ActorAgent,
		trace.ToolSelectionData(uses, toolName, args))
}

// traceDenied records a policy denial as a system_error, matching the internal loop's fail-closed
// path (the tool never ran, so no tool_selection/tool_execution is emitted).
func (d *PolicyDispatcher) traceDenied(ctx context.Context, stepID string, err error) {
	if d.rec == nil || d.runID == "" {
		return
	}
	if de, ok := policy.AsDenied(err); ok {
		_, _ = d.rec.Append(ctx, d.runID, stepID, trace.EventSystemError, trace.ActorSystem, de.TraceData())
	}
}

func (d *PolicyDispatcher) traceExecution(ctx context.Context, stepID, uses, toolName string, meta tools.ToolCallMeta, callErr error) {
	if d.rec == nil || d.runID == "" {
		return
	}
	_, _ = d.rec.Append(ctx, d.runID, stepID, trace.EventToolExecution, trace.ActorAgent,
		trace.ToolExecutionData(uses, toolName, meta.DurationMs, meta.CostUSD, callErr != nil))
}
