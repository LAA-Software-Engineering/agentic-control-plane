package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/LAA-Software-Engineering/terfyn/internal/execir"
	"github.com/LAA-Software-Engineering/terfyn/internal/lang/lower"
	"github.com/LAA-Software-Engineering/terfyn/internal/policy"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
	"github.com/LAA-Software-Engineering/terfyn/internal/telemetry"
	"github.com/LAA-Software-Engineering/terfyn/internal/trace"
)

// engineInvoker is the engine-backed [execir.Invoker] (issue #257): it runs each
// execir leaf through the SAME per-step executors the DAG runtime uses
// (runToolStep/runAgentStep), so policy (CheckRun admit + CheckToolCall), tool
// input/output enforcement (schema #204, byte limits/truncation #117), cost, and
// trace are reproduced without duplication — that reuse is what makes the two
// paths observably identical on a completing graph.
//
// It mirrors executeOneStep + commitDAGStepSuccess minus the parts that are out
// of Phase 1's bar: no per-step checkpoint (the execir path is non-resumable
// here), no HITL/suspend (that stays on the DAG path, #258), and no
// interpolation (execir evaluates argument Values itself — the ADR 002 §5 point).
//
// Concurrency: a Graph or Fork invokes from several goroutines. The shared
// [liveCost] is already mutex-guarded; ictx (accumulated step results, read only
// after Interp.Run returns) and the run-step writes are guarded by mu.
type engineInvoker struct {
	e            *Executor
	in           RunInput
	wf           *spec.WorkflowResource
	wfPol        policy.PolicyEvaluator
	runHandle    *telemetry.RunHandle
	cost         *liveCost
	runStartedAt time.Time

	mu   sync.Mutex
	ictx Context // Input + accumulated Steps, for buildWorkflowOutput after the run
}

// runViaExecIR lowers the workflow to an execir.Program and runs it via
// execir.Interp with an engine-backed Invoker, then finishes through the shared
// success tail so output/cost/finish bookkeeping match the DAG path (issue #257).
func (e *Executor) runViaExecIR(ctx context.Context, in RunInput, wf *spec.WorkflowResource, wfPol policy.PolicyEvaluator, runStartedAt time.Time, runHandle *telemetry.RunHandle) error {
	prog, diags := lower.LowerWorkflowResource(wf)
	if err := diags.AsError(); err != nil {
		return e.failRun(ctx, in, fmt.Errorf("engine: lower workflow %q to execir: %w", in.WorkflowName, err), 0)
	}
	cost := &liveCost{}
	inv := newEngineInvoker(e, in, wf, wfPol, runHandle, cost, runStartedAt)
	interp := &execir.Interp{Invoker: inv, MaxConcurrency: in.MaxConcurrentSteps}
	if _, err := interp.Run(ctx, prog, in.Input); err != nil {
		return e.failRun(ctx, in, err, cost.get())
	}
	return e.finishRunSucceeded(ctx, in, wf, inv.snapshotIctx(), cost.get())
}

func newEngineInvoker(e *Executor, in RunInput, wf *spec.WorkflowResource, wfPol policy.PolicyEvaluator, runHandle *telemetry.RunHandle, cost *liveCost, runStartedAt time.Time) *engineInvoker {
	return &engineInvoker{
		e:            e,
		in:           in,
		wf:           wf,
		wfPol:        wfPol,
		runHandle:    runHandle,
		cost:         cost,
		runStartedAt: runStartedAt,
		ictx:         Context{Input: in.Input, Steps: map[string]StepResult{}},
	}
}

// snapshotIctx returns the accumulated interpolation context for output building
// once the run has finished (no concurrent invokers remain).
func (a *engineInvoker) snapshotIctx() Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Context{Input: a.ictx.Input, Steps: cloneStepResults(a.ictx.Steps)}
}

func (a *engineInvoker) InvokeTool(ctx context.Context, site execir.CallSite, uses string, args map[string]any) (any, error) {
	step := spec.WorkflowStep{ID: site.Bind, Uses: uses}
	return a.run(ctx, step, args, func(pctx policy.RunContext) (map[string]any, float64, error) {
		out, meta, err := a.e.runToolStep(ctx, a.runHandle, a.wfPol, a.wf, a.in.RunID, step, args, pctx, uses, args)
		return out, meta.CostUSD, err
	})
}

func (a *engineInvoker) InvokeAgent(ctx context.Context, site execir.CallSite, agentName string, args map[string]any) (any, error) {
	step := spec.WorkflowStep{ID: site.Bind, Agent: agentName}
	ar, ok := a.e.Graph.Agents[agentName]
	if !ok || ar == nil {
		return nil, fmt.Errorf("engine: unknown agent %q", agentName)
	}
	return a.run(ctx, step, args, func(pctx policy.RunContext) (map[string]any, float64, error) {
		out, meta, err := a.e.runAgentStep(ctx, a.runHandle, a.wfPol, a.wf, a.in.RunID, step, args, pctx, ar)
		return out, meta.CostUSD, err
	})
}

// InvokeWorkflow is not supported on the execir path in Phase 1: a workflow: step
// re-enters the DAG runtime (runSubworkflowStep is bound to *dagRuntime + nested
// checkpoint state), which is out of scope for this non-resumable flag. The DAG
// path still handles workflow: steps in production; the differential corpus does
// not use them.
func (a *engineInvoker) InvokeWorkflow(_ context.Context, _ execir.CallSite, workflow string, _ map[string]any) (any, error) {
	return nil, fmt.Errorf("engine: workflow: step %q is not yet supported on the execir run path (issue #257 Phase 1; use the DAG path)", workflow)
}

// run wraps one leaf invocation with the admit/persist/cost/commit envelope the
// DAG applies per step (executeOneStep + commitDAGStepSuccess), so the two paths
// emit the same rows, trace, and cost. dispatch performs the actual leaf call and
// returns its output and cost.
func (a *engineInvoker) run(ctx context.Context, step spec.WorkflowStep, args map[string]any, dispatch func(policy.RunContext) (map[string]any, float64, error)) (any, error) {
	qid := a.e.qualID(step.ID)
	inJSON, _ := json.Marshal(args)

	// Pre-admit against already-accumulated cost/elapsed (mirrors executeOneStep).
	admitCtx := policy.RunContext{
		StartedAt:          a.runStartedAt,
		Elapsed:            a.e.now().Sub(a.runStartedAt),
		AccumulatedCostUSD: a.cost.get(),
		ApprovedActions:    append([]string(nil), a.in.ApprovedActions...),
	}
	if err := a.wfPol.CheckRun(ctx, admitCtx); err != nil {
		a.e.appendCostLimitHit(ctx, a.in.RunID, qid, err)
		a.failStepRow(ctx, qid, inJSON, err, 0)
		return nil, err
	}

	started := a.e.now()
	if err := a.e.Store.UpsertRunStep(ctx, state.RunStep{
		RunID: a.in.RunID, StepID: qid, Status: "running", StartedAt: &started, InputJSON: string(inJSON),
	}); err != nil {
		return nil, fmt.Errorf("engine: upsert step %q: %w", step.ID, err)
	}

	out, stepCost, err := dispatch(admitCtx)
	if err != nil {
		a.failStepRow(ctx, qid, inJSON, err, stepCost)
		return nil, err
	}

	// Commit cost, then re-check the run budget so two in-flight branches cannot
	// jointly exceed maxTotalCostUsd (mirrors commitDAGStepSuccess).
	total := a.cost.add(stepCost)
	commitCtx := policy.RunContext{
		StartedAt:          a.runStartedAt,
		Elapsed:            a.e.now().Sub(a.runStartedAt),
		AccumulatedCostUSD: total,
		ApprovedActions:    append([]string(nil), a.in.ApprovedActions...),
	}
	if err := a.wfPol.CheckRun(ctx, commitCtx); err != nil {
		a.e.appendCostLimitHit(ctx, a.in.RunID, qid, err)
		a.failStepRow(ctx, qid, inJSON, err, stepCost)
		return nil, err
	}

	finished := a.e.now()
	a.mu.Lock()
	a.ictx.Steps[step.ID] = StepResult{
		Output: out,
		Meta:   map[string]any{"costUsd": stepCost, "durationMs": finished.Sub(started).Milliseconds()},
	}
	a.mu.Unlock()

	outJSON, _ := json.Marshal(out)
	if err := a.e.Store.UpsertRunStep(ctx, state.RunStep{
		RunID: a.in.RunID, StepID: qid, Status: "succeeded",
		StartedAt: &started, FinishedAt: &finished, InputJSON: string(inJSON), OutputJSON: string(outJSON), CostUSD: stepCost,
	}); err != nil {
		return nil, fmt.Errorf("engine: upsert step %q: %w", step.ID, err)
	}
	return out, nil
}

// failStepRow records a failed run-step row and a run_error trace event, matching
// the DAG's per-step failure bookkeeping (runDAGStep). The tool/agent executors
// already emit their own system_error/limit_hit for denials before returning.
func (a *engineInvoker) failStepRow(ctx context.Context, qid string, inJSON []byte, err error, stepCost float64) {
	finished := a.e.now()
	started := finished
	_ = a.e.Store.UpsertRunStep(ctx, state.RunStep{
		RunID: a.in.RunID, StepID: qid, Status: "failed",
		StartedAt: &started, FinishedAt: &finished, InputJSON: string(inJSON), ErrorText: err.Error(), CostUSD: stepCost,
	})
	if a.e.Trace != nil {
		_, _ = a.e.Trace.Append(ctx, a.in.RunID, qid, trace.EventRunError, trace.ActorSystem, runErrorTraceData(qid, err))
	}
}

var _ execir.Invoker = (*engineInvoker)(nil)
