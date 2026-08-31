package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LAA-Software-Engineering/terfyn/internal/execir"
	"github.com/LAA-Software-Engineering/terfyn/internal/lang/lower"
	"github.com/LAA-Software-Engineering/terfyn/internal/policy"
	"github.com/LAA-Software-Engineering/terfyn/internal/render"
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
// It mirrors executeOneStep + commitDAGStepSuccess, reusing the DAG's HITL
// machinery (BuildHitlGateWithEvaluator / resolvePendingHitl / approvalHitlGate)
// so suspend/resume (#258) behaves identically. There is no interpolation (execir
// evaluates argument Values itself — the ADR 002 §5 point) and no per-node
// checkpoint: the durable state is the interpreter's completed-leaf memo, written
// once at the suspend point. Concurrent per-branch suspend and subworkflow
// re-entry stay out (#270 Tier B).
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

	// resuming is true on a resume; pending is the suspended gate loaded from the
	// checkpoint, anchored by its execir CallKey (issue #258).
	resuming bool

	mu      sync.Mutex
	ictx    Context           // Input + accumulated Steps, for buildWorkflowOutput after the run
	pending *PendingHitlState // the leaf awaiting a decision (seeded on resume; set on a fresh suspend)
	// resumeApproved is the uses string an operator just approved on resume; it is
	// added to ApprovedActions for the resolved dispatch so its CheckToolCall
	// passes (mirroring the DAG's executeOneStep). Sequential (the gate resolves on
	// one goroutine), so a plain field is safe.
	resumeApproved string
}

// approvedActions returns the run's approved actions plus any action just resolved
// on resume.
func (a *engineInvoker) approvedActions() []string {
	out := append([]string(nil), a.in.ApprovedActions...)
	if a.resumeApproved != "" {
		out = append(out, a.resumeApproved)
	}
	return out
}

// runViaExecIR lowers the workflow to an execir.Program and runs it via
// execir.Interp with an engine-backed Invoker (issue #257), now durable (#258):
// on a resume it seeds the interpreter's completed-leaf memo so replay never
// re-issues a side effect, and on a HITL/approval suspend it checkpoints that
// memo and returns ErrInterrupted. Completion finishes through the shared
// success tail so output/cost bookkeeping matches the DAG path.
func (e *Executor) runViaExecIR(ctx context.Context, in RunInput, wf *spec.WorkflowResource, wfPol policy.PolicyEvaluator, runStartedAt time.Time, runHandle *telemetry.RunHandle) error {
	prog, diags := lower.LowerWorkflowResource(wf)
	if err := diags.AsError(); err != nil {
		return e.failRun(ctx, in, fmt.Errorf("engine: lower workflow %q to execir: %w", in.WorkflowName, err), 0)
	}
	cost := &liveCost{}
	inv := newEngineInvoker(e, in, wf, wfPol, runHandle, cost, runStartedAt)

	var seed *execir.RunState
	if in.Resume {
		ictx, totalCost, state, err := e.loadExecResumeState(ctx, in, wf)
		if err != nil {
			return e.failRun(ctx, in, err, 0)
		}
		inv.resuming = true
		inv.ictx = ictx // seed completed steps for output + interpolation
		inv.pending = ictx.PendingHitl
		cost.add(totalCost) // memoized leaves are not re-invoked, so their cost is already counted
		seed = state
	}

	interp := &execir.Interp{Invoker: inv, MaxConcurrency: in.MaxConcurrentSteps}
	_, runState, err := interp.RunResumable(ctx, prog, in.Input, seed)
	if err != nil {
		return e.failRun(ctx, in, err, cost.get())
	}
	if runState.Suspended {
		return e.suspendExecIR(ctx, in, wf, inv, runState, cost.get())
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
	key := execir.CallKey(site)

	// Resume at the suspended gate: apply the operator decision, then dispatch the
	// resolved call. resolvePendingHitl re-runs CheckToolCall for the resolved
	// uses and emits the decision trace (audit parity with the DAG path).
	if a.resuming && a.pending != nil && a.pending.ExecKey == key {
		p := a.pending
		a.pending = nil
		step := spec.WorkflowStep{ID: site.Bind, Uses: p.Uses}
		resolvedUses, resolvedWith, rerr := a.e.resolvePendingHitl(ctx, a.in, step, a.wfPol, a.pctxNow(), p)
		if rerr != nil {
			return nil, rerr
		}
		a.resumeApproved = resolvedUses
		defer func() { a.resumeApproved = "" }()
		return a.dispatchTool(ctx, site, resolvedUses, resolvedWith)
	}

	// Fresh run: a gated uses: call suspends (or, under auto-approve, records and
	// proceeds). On a resume past the gate, the pending is already cleared and the
	// call dispatches normally.
	if !a.resuming {
		step := spec.WorkflowStep{ID: site.Bind, Uses: uses}
		// A gate-builder error is advisory only — the authoritative deny is
		// CheckToolCall inside runToolStep (which emits the system_error trace),
		// so on error fall through to dispatch, exactly as the DAG's
		// executeOneStep discards maybeInterruptForHitl's error when it does not
		// interrupt.
		if gate, gerr := a.buildToolGate(step, uses, args); gerr == nil && gate != nil {
			if a.in.Hitl.AutoApprove {
				a.e.recordAutoApproveHitl(ctx, a.in.RunID, step, 0, *gate, a.in.Hitl.Actor)
			} else {
				a.pending = &PendingHitlState{StepID: step.ID, Uses: gate.Uses, With: gate.With, Review: gate.Review, ExecKey: key}
				a.emitHitlRequest(ctx, step, gate)
				return nil, execir.ErrSuspend
			}
		}
	}
	return a.dispatchTool(ctx, site, uses, args)
}

// dispatchTool runs a (possibly HITL-resolved) tool call through the shared
// per-step envelope.
func (a *engineInvoker) dispatchTool(ctx context.Context, site execir.CallSite, uses string, args map[string]any) (any, error) {
	step := spec.WorkflowStep{ID: site.Bind, Uses: uses}
	return a.run(ctx, step, args, func(pctx policy.RunContext) (map[string]any, float64, error) {
		out, meta, err := a.e.runToolStep(ctx, a.runHandle, a.wfPol, a.wf, a.in.RunID, step, args, pctx, uses, args)
		return out, meta.CostUSD, err
	})
}

// InvokeApproval runs a workflow-level approval node (#195/#258): a fresh run
// suspends for a decision; a resume applies it and publishes the reviewed
// payload as the node's output.
func (a *engineInvoker) InvokeApproval(ctx context.Context, site execir.CallSite, info execir.ApprovalInfo, args map[string]any) (any, error) {
	key := execir.CallKey(site)
	step := approvalStepFromInfo(site.Bind, info)

	if a.resuming && a.pending != nil && a.pending.ExecKey == key {
		p := a.pending
		a.pending = nil
		_, resolved, rerr := a.e.resolvePendingHitl(ctx, a.in, step, a.wfPol, a.pctxNow(), p)
		if rerr != nil {
			return nil, rerr
		}
		if resolved == nil {
			resolved = map[string]any{}
		}
		return a.run(ctx, step, resolved, func(policy.RunContext) (map[string]any, float64, error) { return resolved, 0, nil })
	}

	gate := approvalHitlGate(step, args)
	if a.in.Hitl.AutoApprove {
		a.e.recordAutoApproveHitl(ctx, a.in.RunID, step, 0, gate, a.in.Hitl.Actor)
		if args == nil {
			args = map[string]any{}
		}
		return a.run(ctx, step, args, func(policy.RunContext) (map[string]any, float64, error) { return args, 0, nil })
	}
	a.pending = &PendingHitlState{StepID: step.ID, Uses: gate.Uses, With: gate.With, Review: gate.Review, Kind: PendingHitlKindApproval, ExecKey: key}
	a.emitHitlRequest(ctx, step, &gate)
	return nil, execir.ErrSuspend
}

// approvalStepFromInfo reconstructs a spec.WorkflowStep carrying the approval
// presentation so the shared approval/HITL helpers (approvalHitlGate,
// resolvePendingHitl, recordAutoApproveHitl) apply unchanged.
func approvalStepFromInfo(bind string, info execir.ApprovalInfo) spec.WorkflowStep {
	return spec.WorkflowStep{
		ID: bind,
		Approval: &spec.WorkflowApprovalValue{
			Enabled: true,
			Config:  &spec.WorkflowApprovalConfig{Description: info.Description, RedactKeys: info.RedactKeys},
		},
	}
}

func (a *engineInvoker) pctxNow() policy.RunContext {
	return policy.RunContext{
		StartedAt:          a.runStartedAt,
		Elapsed:            a.e.now().Sub(a.runStartedAt),
		AccumulatedCostUSD: a.cost.get(),
		ApprovedActions:    a.approvedActions(),
	}
}

// buildToolGate returns the HITL gate for a uses: call, or nil when the call is
// ungated. Non-nil means the fresh run must suspend (or auto-record).
func (a *engineInvoker) buildToolGate(step spec.WorkflowStep, uses string, args map[string]any) (*policy.HitlGate, error) {
	return policy.BuildHitlGateWithEvaluator(a.e.Graph, a.wfPol, policySpecFromEvaluator(a.wfPol), policy.ToolCallContext{
		Run: a.pctxNow(), StepID: step.ID, Uses: uses, With: args,
	})
}

// emitHitlRequest mirrors interruptForHitlGate's request-created trace + approval
// span so the audit chain matches the DAG path across a suspend.
func (a *engineInvoker) emitHitlRequest(ctx context.Context, step spec.WorkflowStep, gate *policy.HitlGate) {
	if a.runHandle != nil {
		endApproval := a.runHandle.StartApproval(telemetry.ApprovalAttrs{RunID: a.in.RunID, Uses: gate.Uses})
		endApproval()
	}
	if a.e.Trace == nil {
		return
	}
	redacted := policy.RedactHitlArgs(gate.With, gate.Review.RedactKeys)
	data := map[string]any{
		"uses":             gate.Uses,
		"with":             redacted,
		"description":      gate.Review.Description,
		"allowedDecisions": gate.Review.AllowedDecisions,
		"allowedSwitchTo":  gate.Review.SwitchTargets,
	}
	_, _ = a.e.Trace.Append(ctx, a.in.RunID, step.ID, trace.EventHitlRequestCreated, trace.ActorSystem, data)
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
		ApprovedActions:    a.approvedActions(),
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
		ApprovedActions:    a.approvedActions(),
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

// suspendExecIR persists the execir durable state (completed-leaf memo + control
// + pending gate) and marks the run interrupted (issue #258), mirroring the DAG
// interruptForHitlGate's status/otel/trace so the audit chain matches.
func (e *Executor) suspendExecIR(ctx context.Context, in RunInput, wf *spec.WorkflowResource, inv *engineInvoker, runState *execir.RunState, totalCost float64) error {
	if inv.pending == nil {
		return e.failRun(ctx, in, fmt.Errorf("engine: execir run suspended without a pending gate"), totalCost)
	}
	ictx := inv.snapshotIctx()
	ictx.PendingHitl = inv.pending
	if inv.runHandle != nil {
		inv.runHandle.MarkInterrupted()
		ref := inv.runHandle.SpanRef()
		ictx.OtelInterrupt = &ref
	}
	stepIndex := execStepIndex(wf, inv.pending.StepID)
	if err := e.saveExecCheckpoint(ctx, wf, in, stepIndex, ictx, totalCost, runState); err != nil {
		return e.failRun(ctx, in, fmt.Errorf("engine: save execir checkpoint: %w", err), totalCost)
	}
	if err := e.Store.UpdateRunStatus(ctx, in.RunID, state.RunStatusInterrupted); err != nil {
		return fmt.Errorf("engine: mark run interrupted: %w", err)
	}
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, in.RunID, e.qualID(inv.pending.StepID), trace.EventRunError, trace.ActorSystem, map[string]any{
			"stepIndex": stepIndex, "stepId": inv.pending.StepID, "reason": traceInterruptReasonHITL, "interrupted": true,
		})
	}
	return ErrInterrupted
}

// execStepIndex finds the workflow index of a step id (checkpoint metadata only).
func execStepIndex(wf *spec.WorkflowResource, stepID string) int {
	if wf == nil {
		return -1
	}
	for i := range wf.Spec.Steps {
		if strings.TrimSpace(wf.Spec.Steps[i].ID) == strings.TrimSpace(stepID) {
			return i
		}
	}
	return -1
}

// saveExecCheckpoint writes an execir checkpoint (ExecIR marker + memo/control),
// reusing the size enforcement the DAG checkpoint uses.
func (e *Executor) saveExecCheckpoint(ctx context.Context, wf *spec.WorkflowResource, in RunInput, stepIndex int, ictx Context, totalCost float64, runState *execir.RunState) error {
	payload := checkpointPayload{
		Version:       checkpointPayloadVersion,
		Input:         ictx.Input,
		Steps:         ictx.Steps,
		Completed:     completedStepIDs(ictx.Steps),
		TotalCostUSD:  totalCost,
		PendingHitl:   ictx.PendingHitl,
		OtelInterrupt: ictx.OtelInterrupt,
		ExecIR:        true,
		ExecMemo:      runState.Memo,
		ExecControl:   runState.Control,
	}
	if payload.Input == nil {
		payload.Input = map[string]any{}
	}
	if payload.Steps == nil {
		payload.Steps = map[string]StepResult{}
	}
	ctxJSON, err := render.MarshalStableJSON(payload)
	if err != nil {
		return fmt.Errorf("engine: marshal execir checkpoint: %w", err)
	}
	if len(ctxJSON) > maxCheckpointContextBytes {
		return fmt.Errorf("engine: execir checkpoint context exceeds absolute maximum %d bytes", maxCheckpointContextBytes)
	}
	stepID := e.qualID(strings.TrimSpace(in.WorkflowName))
	if inv := strings.TrimSpace(ictx.PendingHitl.StepID); inv != "" {
		stepID = e.qualID(inv)
	}
	if err := e.enforceCheckpointSize(ctx, wf, in.RunID, stepID, string(ctxJSON)); err != nil {
		return err
	}
	return e.Store.SaveCheckpoint(ctx, state.RunCheckpoint{
		RunID:       in.RunID,
		StepIndex:   stepIndex,
		StepID:      stepID,
		ContextJSON: string(ctxJSON),
		Status:      state.CheckpointStatusInterrupted,
		CreatedAt:   e.now(),
	})
}

// loadExecResumeState hydrates the seeded interpolation context (completed steps
// + pending gate), the accumulated cost, and the interpreter durable state from
// the latest execir checkpoint.
func (e *Executor) loadExecResumeState(ctx context.Context, in RunInput, wf *spec.WorkflowResource) (Context, float64, *execir.RunState, error) {
	cp, err := e.Store.GetLatestCheckpoint(ctx, in.RunID)
	if err != nil {
		return Context{}, 0, nil, fmt.Errorf("engine: load execir checkpoint: %w", err)
	}
	switch cp.Status {
	case state.CheckpointStatusRunning, state.CheckpointStatusInterrupted:
	default:
		return Context{}, 0, nil, fmt.Errorf("engine: checkpoint status %q is not resumable", cp.Status)
	}
	ictx, totalCost, err := unmarshalCheckpointPayload(cp.ContextJSON, e.Graph, wf, cp.StepIndex)
	if err != nil {
		return Context{}, 0, nil, err
	}
	var payload checkpointPayload
	if err := json.Unmarshal([]byte(cp.ContextJSON), &payload); err != nil {
		return Context{}, 0, nil, fmt.Errorf("engine: unmarshal execir checkpoint: %w", err)
	}
	if !payload.ExecIR {
		return Context{}, 0, nil, fmt.Errorf("engine: resume routed to execir but checkpoint is not an execir checkpoint")
	}
	return ictx, totalCost, &execir.RunState{Memo: payload.ExecMemo, Control: payload.ExecControl}, nil
}

// resumeIsExecIR reports whether the run's latest checkpoint was written by the
// execir path, so Run can route a resume back to it.
func (e *Executor) resumeIsExecIR(ctx context.Context, runID string) (bool, error) {
	cp, err := e.Store.GetLatestCheckpoint(ctx, runID)
	if err != nil {
		return false, fmt.Errorf("engine: load checkpoint: %w", err)
	}
	var payload checkpointPayload
	if err := json.Unmarshal([]byte(cp.ContextJSON), &payload); err != nil {
		return false, fmt.Errorf("engine: unmarshal checkpoint: %w", err)
	}
	return payload.ExecIR, nil
}

var _ execir.Invoker = (*engineInvoker)(nil)
