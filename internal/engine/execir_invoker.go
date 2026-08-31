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

	mu             sync.Mutex
	ictx           Context           // Input + accumulated Steps, for buildWorkflowOutput after the run
	suspendClaimed bool              // one suspension cause per run cycle (first-wins across gates/subworkflows)
	pending        *PendingHitlState // a direct gate awaiting a decision (seeded on resume; set first-wins on a fresh suspend)
	nested         *nestedSuspension // a suspended subworkflow frame (set first-wins on a fresh suspend, #270)
	nestedSeed     *NestedRunState   // a suspended subworkflow to resume (seeded from the checkpoint on resume)
}

// nestedSuspension is a subworkflow that suspended: the callee's completed steps +
// pending (ictx) and its interpreter durable state (memo/control), anchored to the
// parent's InvokeWorkflow CallSite key.
type nestedSuspension struct {
	key    string
	stepID string // the parent's workflow: step id (NestedRunState.StepID anchor)
	callee string
	ictx   Context
	state  *execir.RunState
}

// approvedActions returns the run's approved actions plus, for a resolved resume
// dispatch, the just-approved uses (extra) — so its CheckToolCall passes, mirroring
// the DAG's executeOneStep. extra is passed per-call (not shared state), which is
// what keeps concurrent per-branch dispatch race-free.
func (a *engineInvoker) approvedActions(extra string) []string {
	out := append([]string(nil), a.in.ApprovedActions...)
	if extra != "" {
		out = append(out, extra)
	}
	return out
}

// claimPending atomically returns and clears the pending gate iff it is anchored
// to key (the leaf being resumed); nil otherwise. Concurrency-safe: a Graph/Fork
// resumes and re-runs branches from several goroutines.
func (a *engineInvoker) claimPending(key string) *PendingHitlState {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending != nil && a.pending.ExecKey == key {
		p := a.pending
		a.pending = nil
		return p
	}
	return nil
}

// setPendingIfFirst records a direct gate as the run's one suspension cause iff
// none is claimed yet (first-suspend wins), returning whether it won. A racing
// second gate is dropped and re-runs on resume — the DAG's cancel-on-first-interrupt.
func (a *engineInvoker) setPendingIfFirst(p *PendingHitlState) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.suspendClaimed {
		return false
	}
	a.suspendClaimed = true
	a.pending = p
	return true
}

// setNestedIfFirst records a suspended subworkflow as the run's one suspension
// cause iff none is claimed yet.
func (a *engineInvoker) setNestedIfFirst(n *nestedSuspension) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.suspendClaimed {
		return false
	}
	a.suspendClaimed = true
	a.nested = n
	return true
}

func (a *engineInvoker) getPending() *PendingHitlState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pending
}

func (a *engineInvoker) getNested() *nestedSuspension {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.nested
}

// claimNestedSeed returns the seeded subworkflow frame to resume iff it is
// anchored to key, clearing it so a re-run branch does not double-resume.
func (a *engineInvoker) claimNestedSeed(key string) *NestedRunState {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.nestedSeed != nil && a.nestedSeed.ExecKey == key {
		ns := a.nestedSeed
		a.nestedSeed = nil
		return ns
	}
	return nil
}

// runViaExecIR lowers the workflow to an execir.Program and runs it via
// execir.Interp with an engine-backed Invoker (issue #257), now durable (#258):
// on a resume it seeds the interpreter's completed-leaf memo so replay never
// re-issues a side effect, and on a HITL/approval suspend it checkpoints that
// memo and returns ErrInterrupted. Completion finishes through the shared
// success tail so output/cost bookkeeping matches the DAG path.
func (e *Executor) runViaExecIR(ctx context.Context, in RunInput, wf *spec.WorkflowResource, wfPol policy.PolicyEvaluator, runStartedAt time.Time, runHandle *telemetry.RunHandle) error {
	// Prefer the PINNED program (issue #260): the exact execir.Program captured in
	// the deployment snapshot — for a fresh Invoke from resolved config, for a
	// resume hydrated from the snapshot — so the runtime never re-lowers from
	// source (ADR 001). Fall back to lowering only when no program was pinned
	// (e.g. a store without artifact support).
	prog := e.Executables[in.WorkflowName]
	if prog == nil {
		lowered, diags := lower.LowerWorkflowResource(wf)
		if err := diags.AsError(); err != nil {
			return e.failRun(ctx, in, fmt.Errorf("engine: lower workflow %q to execir: %w", in.WorkflowName, err), 0)
		}
		prog = lowered
	}
	cost := &liveCost{}
	inv := newEngineInvoker(e, in, wf, wfPol, runHandle, cost, runStartedAt)

	var seed *execir.RunState
	if in.Resume {
		ictx, totalCost, state, nested, err := e.loadExecResumeState(ctx, in, wf)
		if err != nil {
			return e.failRun(ctx, in, err, 0)
		}
		inv.resuming = true
		inv.ictx = ictx // seed completed steps for output + interpolation
		inv.pending = ictx.PendingHitl
		inv.nestedSeed = nested // a suspended subworkflow to resume (#270)
		cost.add(totalCost)     // memoized leaves are not re-invoked, so their cost is already counted
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
	// uses and emits the decision trace (audit parity with the DAG path). claim
	// is atomic so, under a concurrent Graph/Fork resume, only the matching branch
	// resolves it.
	if p := a.claimPending(key); p != nil {
		step := spec.WorkflowStep{ID: site.Bind, Uses: p.Uses}
		resolvedUses, resolvedWith, rerr := a.e.resolvePendingHitl(ctx, a.in, step, a.wfPol, a.pctx(""), p)
		if rerr != nil {
			return nil, rerr
		}
		return a.dispatchTool(ctx, site, resolvedUses, resolvedWith, resolvedUses)
	}

	// Fresh run: a gated uses: call suspends (or, under auto-approve, records and
	// proceeds). On a resume, gates are NOT re-suspended (a.resuming): the one
	// pending gate is resolved above, and any other gated call dispatches — where
	// CheckToolCall fails closed if still unapproved (same as the DAG).
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
				if a.setPendingIfFirst(&PendingHitlState{StepID: step.ID, Uses: gate.Uses, With: gate.With, Review: gate.Review, ExecKey: key}) {
					a.emitHitlRequest(ctx, step, gate)
				}
				return nil, execir.ErrSuspend
			}
		}
	}
	return a.dispatchTool(ctx, site, uses, args, "")
}

// dispatchTool runs a (possibly HITL-resolved) tool call through the shared
// per-step envelope. extraApproved is the just-approved uses on a resume (empty
// for a fresh dispatch).
func (a *engineInvoker) dispatchTool(ctx context.Context, site execir.CallSite, uses string, args map[string]any, extraApproved string) (any, error) {
	step := spec.WorkflowStep{ID: site.Bind, Uses: uses}
	return a.run(ctx, step, args, extraApproved, func(pctx policy.RunContext) (map[string]any, float64, error) {
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

	if p := a.claimPending(key); p != nil {
		_, resolved, rerr := a.e.resolvePendingHitl(ctx, a.in, step, a.wfPol, a.pctx(""), p)
		if rerr != nil {
			return nil, rerr
		}
		if resolved == nil {
			resolved = map[string]any{}
		}
		return a.run(ctx, step, resolved, "", func(policy.RunContext) (map[string]any, float64, error) { return resolved, 0, nil })
	}

	// An unclaimed approval node always needs a decision (there is no dispatch
	// fallback as for a tool gate): auto-approve records and proceeds, otherwise it
	// suspends — whether fresh or a not-yet-resolved approval reached on resume.
	gate := approvalHitlGate(step, args)
	if a.in.Hitl.AutoApprove {
		a.e.recordAutoApproveHitl(ctx, a.in.RunID, step, 0, gate, a.in.Hitl.Actor)
		if args == nil {
			args = map[string]any{}
		}
		return a.run(ctx, step, args, "", func(policy.RunContext) (map[string]any, float64, error) { return args, 0, nil })
	}
	if a.setPendingIfFirst(&PendingHitlState{StepID: step.ID, Uses: gate.Uses, With: gate.With, Review: gate.Review, Kind: PendingHitlKindApproval, ExecKey: key}) {
		a.emitHitlRequest(ctx, step, &gate)
	}
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

func (a *engineInvoker) pctx(extraApproved string) policy.RunContext {
	return policy.RunContext{
		StartedAt:          a.runStartedAt,
		Elapsed:            a.e.now().Sub(a.runStartedAt),
		AccumulatedCostUSD: a.cost.get(),
		ApprovedActions:    a.approvedActions(extraApproved),
	}
}

// buildToolGate returns the HITL gate for a uses: call, or nil when the call is
// ungated. Non-nil means the fresh run must suspend (or auto-record).
func (a *engineInvoker) buildToolGate(step spec.WorkflowStep, uses string, args map[string]any) (*policy.HitlGate, error) {
	return policy.BuildHitlGateWithEvaluator(a.e.Graph, a.wfPol, policySpecFromEvaluator(a.wfPol), policy.ToolCallContext{
		Run: a.pctx(""), StepID: step.ID, Uses: uses, With: args,
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
	return a.run(ctx, step, args, "", func(pctx policy.RunContext) (map[string]any, float64, error) {
		out, meta, err := a.e.runAgentStep(ctx, a.runHandle, a.wfPol, a.wf, a.in.RunID, step, args, pctx, ar)
		return out, meta.CostUSD, err
	})
}

// InvokeWorkflow runs a subworkflow as a NESTED execir run (issue #270): the
// callee lowers to its own execir.Program and executes through a child
// engineInvoker that shares the run-wide cost, nests trace/persistence ids, and
// applies the stricter of caller/callee policy — the execir counterpart to the
// DAG's runSubworkflowStep. On the callee suspending, the parent records a nested
// frame (callee memo + pending) and suspends; on resume the child is seeded from
// that frame so its completed inner steps replay, never re-run. A subworkflow that
// COMPLETED is instead replayed via the parent's own memo (invoke wraps this call),
// so it is never re-entered at all.
func (a *engineInvoker) InvokeWorkflow(ctx context.Context, site execir.CallSite, workflow string, args map[string]any) (any, error) {
	key := execir.CallKey(site)
	callee, err := lookupWorkflow(a.e.Graph, workflow)
	if err != nil {
		return nil, fmt.Errorf("engine: step %q: %w", site.Bind, err)
	}

	limitWF := a.wf
	if a.e.rootWF != nil {
		limitWF = a.e.rootWF
	}
	maxDepth := spec.ResolveMaxWorkflowNesting(&a.e.Graph.Spec, &limitWF.Spec)
	depth := a.in.WorkflowDepth + 1
	if depth > maxDepth {
		return nil, fmt.Errorf("engine: step %q: workflow nesting depth %d exceeds maxWorkflowNesting %d", site.Bind, depth, maxDepth)
	}
	if err := a.e.validateWorkflowInputSchema(callee, args); err != nil {
		return nil, fmt.Errorf("engine: step %q subworkflow %q input: %w", site.Bind, workflow, err)
	}

	calleePol, err := compiledWorkflowEvaluator(a.e.ProjectRoot, a.e.Graph, strings.TrimSpace(callee.Spec.Policy), a.e.PinnedGraph)
	if err != nil {
		return nil, err
	}
	wfPol := policy.StricterOf(a.wfPol, calleePol)

	child := *a.e
	prefix, err := qualifyStepID(a.e.stepPrefix, site.Bind)
	if err != nil {
		return nil, err
	}
	child.stepPrefix = prefix
	if a.e.rootWF != nil {
		child.rootWF = a.e.rootWF
	} else {
		child.rootWF = a.wf
	}
	callStack := append(append([]string(nil), a.in.CallStack...), workflow)
	if child.Trace != nil {
		child.Trace = child.Trace.WithCallStack(callStack)
	}

	childIn := a.in
	childIn.WorkflowName = workflow
	childIn.WorkflowDepth = depth
	childIn.CallStack = callStack
	childIn.Resume = false // nested resume is driven by the seeded frame, not RunInput

	childProg := a.e.Executables[workflow]
	if childProg == nil {
		lowered, diags := lower.LowerWorkflowResource(callee)
		if derr := diags.AsError(); derr != nil {
			return nil, fmt.Errorf("engine: lower subworkflow %q to execir: %w", workflow, derr)
		}
		childProg = lowered
	}

	childInv := newEngineInvoker(&child, childIn, callee, wfPol, a.runHandle, a.cost, a.runStartedAt)
	childInv.ictx = Context{Input: args, Steps: map[string]StepResult{}}

	var childSeed *execir.RunState
	if ns := a.claimNestedSeed(key); ns != nil && strings.TrimSpace(ns.Workflow) == workflow {
		childInv.resuming = true
		in := ns.Input
		if in == nil {
			in = args
		}
		steps := ns.Steps
		if steps == nil {
			steps = map[string]StepResult{}
		}
		childInv.ictx = Context{Input: in, Steps: steps, PendingHitl: ns.PendingHitl}
		childInv.pending = ns.PendingHitl
		if ns.PendingHitl != nil {
			childInv.suspendClaimed = false // allow re-claim on resume
		}
		childSeed = &execir.RunState{Memo: ns.ExecMemo, Control: ns.ExecControl}
	} else if a.e.Trace != nil {
		_, _ = a.e.Trace.Append(ctx, a.in.RunID, a.e.qualID(site.Bind), trace.EventWorkflowCallStarted, trace.ActorSystem, map[string]any{
			"workflow": workflow, "depth": depth, "stepId": site.Bind,
		})
	}

	childInterp := &execir.Interp{Invoker: childInv, MaxConcurrency: a.in.MaxConcurrentSteps}
	_, childState, rerr := childInterp.RunResumable(ctx, childProg, childInv.ictx.Input, childSeed)
	if rerr != nil {
		return nil, rerr
	}
	if childState.Suspended {
		snap := childInv.snapshotIctx()
		a.setNestedIfFirst(&nestedSuspension{
			key:    key,
			stepID: site.Bind,
			callee: workflow,
			ictx:   Context{Input: snap.Input, Steps: snap.Steps, PendingHitl: childInv.getPending()},
			state:  childState,
		})
		return nil, execir.ErrSuspend
	}

	out, oerr := buildWorkflowOutput(callee, childInv.snapshotIctx())
	if oerr != nil {
		return nil, fmt.Errorf("engine: step %q subworkflow %q output: %w", site.Bind, workflow, oerr)
	}
	if a.e.Trace != nil {
		_, _ = a.e.Trace.Append(ctx, a.in.RunID, a.e.qualID(site.Bind), trace.EventWorkflowCallFinished, trace.ActorSystem, map[string]any{
			"workflow": workflow, "depth": depth, "stepId": site.Bind,
		})
	}
	a.mu.Lock()
	a.ictx.Steps[site.Bind] = StepResult{Output: out, Meta: map[string]any{}}
	a.mu.Unlock()
	return out, nil
}

// run wraps one leaf invocation with the admit/persist/cost/commit envelope the
// DAG applies per step (executeOneStep + commitDAGStepSuccess), so the two paths
// emit the same rows, trace, and cost. dispatch performs the actual leaf call and
// returns its output and cost.
func (a *engineInvoker) run(ctx context.Context, step spec.WorkflowStep, args map[string]any, extraApproved string, dispatch func(policy.RunContext) (map[string]any, float64, error)) (any, error) {
	qid := a.e.qualID(step.ID)
	inJSON, _ := json.Marshal(args)

	// Pre-admit against already-accumulated cost/elapsed (mirrors executeOneStep).
	admitCtx := a.pctx(extraApproved)
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
	commitCtx := a.pctx(extraApproved)
	commitCtx.AccumulatedCostUSD = total
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
	pending := inv.getPending()
	nested := inv.getNested()
	if pending == nil && nested == nil {
		return e.failRun(ctx, in, fmt.Errorf("engine: execir run suspended without a pending gate or nested subworkflow"), totalCost)
	}
	ictx := inv.snapshotIctx()
	ictx.PendingHitl = pending // nil when the suspension is inside a subworkflow
	anchorStep := ""
	if pending != nil {
		anchorStep = pending.StepID
	}
	if nested != nil {
		ictx.Nested = &NestedRunState{
			StepID:      nested.stepID,
			Workflow:    nested.callee,
			Input:       nested.ictx.Input,
			Steps:       nested.ictx.Steps,
			Completed:   completedStepIDs(nested.ictx.Steps),
			PendingHitl: nested.ictx.PendingHitl,
			ExecKey:     nested.key,
			ExecMemo:    nested.state.Memo,
			ExecControl: nested.state.Control,
		}
		anchorStep = nested.stepID
	}
	if inv.runHandle != nil {
		inv.runHandle.MarkInterrupted()
		ref := inv.runHandle.SpanRef()
		ictx.OtelInterrupt = &ref
	}
	stepIndex := execStepIndex(wf, anchorStep)
	if err := e.saveExecCheckpoint(ctx, wf, in, stepIndex, anchorStep, ictx, totalCost, runState); err != nil {
		return e.failRun(ctx, in, fmt.Errorf("engine: save execir checkpoint: %w", err), totalCost)
	}
	if err := e.Store.UpdateRunStatus(ctx, in.RunID, state.RunStatusInterrupted); err != nil {
		return fmt.Errorf("engine: mark run interrupted: %w", err)
	}
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, in.RunID, e.qualID(anchorStep), trace.EventRunError, trace.ActorSystem, map[string]any{
			"stepIndex": stepIndex, "stepId": anchorStep, "reason": traceInterruptReasonHITL, "interrupted": true,
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
func (e *Executor) saveExecCheckpoint(ctx context.Context, wf *spec.WorkflowResource, in RunInput, stepIndex int, anchorStep string, ictx Context, totalCost float64, runState *execir.RunState) error {
	payload := checkpointPayload{
		Version:       checkpointPayloadVersion,
		Input:         ictx.Input,
		Steps:         ictx.Steps,
		Completed:     completedStepIDs(ictx.Steps),
		TotalCostUSD:  totalCost,
		PendingHitl:   ictx.PendingHitl,
		OtelInterrupt: ictx.OtelInterrupt,
		Nested:        ictx.Nested,
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
	if s := strings.TrimSpace(anchorStep); s != "" {
		stepID = e.qualID(s)
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
func (e *Executor) loadExecResumeState(ctx context.Context, in RunInput, wf *spec.WorkflowResource) (Context, float64, *execir.RunState, *NestedRunState, error) {
	cp, err := e.Store.GetLatestCheckpoint(ctx, in.RunID)
	if err != nil {
		return Context{}, 0, nil, nil, fmt.Errorf("engine: load execir checkpoint: %w", err)
	}
	switch cp.Status {
	case state.CheckpointStatusRunning, state.CheckpointStatusInterrupted:
	default:
		return Context{}, 0, nil, nil, fmt.Errorf("engine: checkpoint status %q is not resumable", cp.Status)
	}
	ictx, totalCost, err := unmarshalCheckpointPayload(cp.ContextJSON, e.Graph, wf, cp.StepIndex)
	if err != nil {
		return Context{}, 0, nil, nil, err
	}
	var payload checkpointPayload
	if err := json.Unmarshal([]byte(cp.ContextJSON), &payload); err != nil {
		return Context{}, 0, nil, nil, fmt.Errorf("engine: unmarshal execir checkpoint: %w", err)
	}
	if !payload.ExecIR {
		return Context{}, 0, nil, nil, fmt.Errorf("engine: resume routed to execir but checkpoint is not an execir checkpoint")
	}
	return ictx, totalCost, &execir.RunState{Memo: payload.ExecMemo, Control: payload.ExecControl}, payload.Nested, nil
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
