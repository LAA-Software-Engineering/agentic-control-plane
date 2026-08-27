package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LAA-Software-Engineering/terfyn/internal/models"
	"github.com/LAA-Software-Engineering/terfyn/internal/policy"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
	"github.com/LAA-Software-Engineering/terfyn/internal/telemetry"
	"github.com/LAA-Software-Engineering/terfyn/internal/tools"
	"github.com/LAA-Software-Engineering/terfyn/internal/trace"
)

// DefaultMaxConcurrentSteps bounds workflow goroutine fan-out (issue #192).
const DefaultMaxConcurrentSteps = 8

// liveCost is the run-wide accumulated cost, shared with nested subworkflow DAGs
// so sibling steps and inner tools admit/commit against one total (issue #194).
type liveCost struct {
	mu    sync.Mutex
	total float64
}

func (c *liveCost) get() float64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}

func (c *liveCost) add(delta float64) float64 {
	if c == nil {
		return delta
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total += delta
	return c.total
}

func (rt *dagRuntime) applyLocalStepCost(step spec.WorkflowStep, stepCost float64) float64 {
	if strings.TrimSpace(step.Workflow) != "" {
		return rt.cost.get()
	}
	return rt.cost.add(stepCost)
}

type dagRuntime struct {
	mu           sync.Mutex
	cost         *liveCost
	completed    map[string]struct{}
	running      map[int]struct{}
	failed       error
	interrupting bool
	interruptErr error
	ictx         Context
}

func (e *Executor) forStep(order int) *Executor {
	if e == nil {
		return nil
	}
	cp := *e
	if e.Trace != nil {
		cp.Trace = e.Trace.WithLogicalOrder(order)
	}
	return &cp
}

func (e *Executor) runWorkflowSteps(
	ctx context.Context,
	in RunInput,
	wf *spec.WorkflowResource,
	wfPol policy.PolicyEvaluator,
	ictx Context,
	cost *liveCost,
	seedCost float64,
	completed map[string]struct{},
	runStartedAt time.Time,
	runHandle *telemetry.RunHandle,
) (Context, float64, error) {
	if completed == nil {
		completed = map[string]struct{}{}
	}
	if ictx.Steps == nil {
		ictx.Steps = map[string]StepResult{}
	}
	if cost == nil {
		cost = &liveCost{total: seedCost}
	}
	steps := wf.Spec.Steps
	rt := &dagRuntime{
		cost:      cost,
		completed: completed,
		running:   map[int]struct{}{},
		ictx:      ictx,
	}

	maxConc := in.MaxConcurrentSteps
	if maxConc <= 0 {
		maxConc = DefaultMaxConcurrentSteps
	}

	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	completion := make(chan struct{}, len(steps)+1)

	trySchedule := func() {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		if rt.failed != nil || rt.interrupting {
			return
		}
		for i, step := range steps {
			id := strings.TrimSpace(step.ID)
			if id == "" {
				continue
			}
			if _, ok := rt.completed[id]; ok {
				continue
			}
			if _, ok := rt.running[i]; ok {
				continue
			}
			if !depsReady(steps, i, rt.completed) {
				continue
			}
			if len(rt.running) >= maxConc {
				return
			}
			rt.running[i] = struct{}{}
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				e.runDAGStep(ctx, parent, cancel, rt, in, wf, wfPol, runStartedAt, runHandle, i, completion)
			}(i)
		}
	}

	trySchedule()
	for {
		rt.mu.Lock()
		nRunning := len(rt.running)
		failed := rt.failed
		interrupting := rt.interrupting
		allDone := workflowFullyComplete(steps, rt.completed)
		rt.mu.Unlock()
		if failed != nil || interrupting || allDone {
			break
		}
		if nRunning == 0 {
			// Roots can finish before this loop samples `running` (fast
			// rendezvous). Schedule again so a join is not reported as stuck.
			trySchedule()
			rt.mu.Lock()
			nRunning = len(rt.running)
			failed = rt.failed
			interrupting = rt.interrupting
			allDone = workflowFullyComplete(steps, rt.completed)
			rt.mu.Unlock()
			if failed != nil || interrupting || allDone {
				break
			}
			if nRunning == 0 {
				wg.Wait()
				return rt.ictx, rt.cost.get(), e.finishWorkflowError(parent, in, fmt.Errorf("engine: workflow graph has no runnable step"), rt.cost.get())
			}
		}
		<-completion
		trySchedule()
	}
	cancel()
	wg.Wait()

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.failed != nil {
		return rt.ictx, rt.cost.get(), e.finishWorkflowError(parent, in, rt.failed, rt.cost.get())
	}
	if rt.interrupting {
		if rt.interruptErr != nil {
			return rt.ictx, rt.cost.get(), rt.interruptErr
		}
		return rt.ictx, rt.cost.get(), ErrInterrupted
	}
	if !workflowFullyComplete(steps, rt.completed) {
		return rt.ictx, rt.cost.get(), e.finishWorkflowError(parent, in, fmt.Errorf("engine: workflow graph incomplete after scheduling"), rt.cost.get())
	}
	return rt.ictx, rt.cost.get(), nil
}

func (e *Executor) finishWorkflowError(ctx context.Context, in RunInput, err error, totalCost float64) error {
	if e != nil && e.nestParent != nil {
		return err
	}
	return e.failRun(ctx, in, err, totalCost)
}

func depsReady(steps []spec.WorkflowStep, i int, completed map[string]struct{}) bool {
	for _, dep := range spec.StepNeedsIDs(steps, i) {
		if _, ok := completed[dep]; !ok {
			return false
		}
	}
	return true
}

func workflowFullyComplete(steps []spec.WorkflowStep, completed map[string]struct{}) bool {
	for _, st := range steps {
		id := strings.TrimSpace(st.ID)
		if id == "" {
			continue
		}
		if _, ok := completed[id]; !ok {
			return false
		}
	}
	return true
}

func cloneStepResults(in map[string]StepResult) map[string]StepResult {
	out := make(map[string]StepResult, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func isContextCanceled(err error) bool {
	return err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

func (e *Executor) runDAGStep(
	ctx context.Context,
	persistCtx context.Context,
	cancel context.CancelFunc,
	rt *dagRuntime,
	in RunInput,
	wf *spec.WorkflowResource,
	wfPol policy.PolicyEvaluator,
	runStartedAt time.Time,
	runHandle *telemetry.RunHandle,
	i int,
	completion chan struct{},
) {
	defer func() {
		completion <- struct{}{}
	}()

	step := wf.Spec.Steps[i]
	ex := e.forStep(i)

	rt.mu.Lock()
	if rt.failed != nil || rt.interrupting {
		delete(rt.running, i)
		rt.mu.Unlock()
		return
	}
	snap := Context{
		Input:         rt.ictx.Input,
		Steps:         cloneStepResults(rt.ictx.Steps),
		PendingHitl:   rt.ictx.PendingHitl,
		OtelInterrupt: rt.ictx.OtelInterrupt,
		Nested:        rt.ictx.Nested,
	}
	rt.mu.Unlock()

	out, stepCost, started, inJSON, pendingCleared, interrupted, err := ex.executeOneStep(ctx, persistCtx, in, wf, wfPol, rt, snap, runStartedAt, runHandle, i, step)

	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.running, i)

	if pendingCleared {
		rt.ictx.PendingHitl = nil
	}
	if interrupted {
		rt.interrupting = true
		rt.interruptErr = err
		cancel()
		return
	}
	if err != nil {
		if (rt.interrupting || rt.failed != nil) && isContextCanceled(err) {
			return
		}
		if rt.failed == nil {
			finished := ex.now()
			_ = ex.Store.UpsertRunStep(persistCtx, state.RunStep{
				RunID:      in.RunID,
				StepID:     ex.qualID(step.ID),
				Status:     "failed",
				StartedAt:  started,
				FinishedAt: &finished,
				InputJSON:  string(inJSON),
				ErrorText:  err.Error(),
				CostUSD:    stepCost,
			})
			if ex.Trace != nil && !isContextCanceled(err) {
				_, _ = ex.Trace.Append(persistCtx, in.RunID, ex.qualID(step.ID), trace.EventRunError, trace.ActorSystem, runErrorTraceData(step.ID, err))
			}
			if strings.Contains(err.Error(), "engine:") {
				rt.failed = err
			} else {
				rt.failed = fmt.Errorf("engine: step %q: %w", step.ID, err)
			}
			rt.applyLocalStepCost(step, stepCost)
		}
		cancel()
		return
	}

	// Merge a successful result before honoring interrupt/failure so a sibling
	// that finished its tool while interruptRun held the lock is not dropped
	// and replayed on resume (issue #192 review).
	if err := ex.commitDAGStepSuccess(persistCtx, rt, in, wf, wfPol, runStartedAt, i, step, out, stepCost, started, inJSON); err != nil {
		if rt.failed == nil {
			rt.failed = err
		}
		cancel()
		return
	}
	if rt.failed != nil {
		return
	}

	if in.InterruptAfterStepIndex != nil && i == *in.InterruptAfterStepIndex {
		rt.interrupting = true
		rt.interruptErr = ex.interruptRun(persistCtx, wf, in, i, step.ID, rt.ictx, rt.cost.get(), runHandle)
		cancel()
		return
	}
	if id := strings.TrimSpace(in.InterruptAfterStepID); id != "" && (id == step.ID || id == ex.qualID(step.ID)) {
		rt.interrupting = true
		rt.interruptErr = ex.interruptRun(persistCtx, wf, in, i, step.ID, rt.ictx, rt.cost.get(), runHandle)
		cancel()
		return
	}
}

// commitDAGStepSuccess records cost, completion, checkpoint, and the succeeded
// row. Caller holds rt.mu. A live CheckRun after applying cost prevents two
// in-flight branches from jointly exceeding maxTotalCostUsd.
func (e *Executor) commitDAGStepSuccess(
	ctx context.Context,
	rt *dagRuntime,
	in RunInput,
	wf *spec.WorkflowResource,
	wfPol policy.PolicyEvaluator,
	runStartedAt time.Time,
	i int,
	step spec.WorkflowStep,
	out map[string]any,
	stepCost float64,
	started *time.Time,
	inJSON []byte,
) error {
	finished := e.now()
	total := rt.applyLocalStepCost(step, stepCost)
	pctx := policy.RunContext{
		StartedAt:          runStartedAt,
		Elapsed:            finished.Sub(runStartedAt),
		AccumulatedCostUSD: total,
		ApprovedActions:    append([]string(nil), in.ApprovedActions...),
	}
	if err := wfPol.CheckRun(ctx, pctx); err != nil {
		e.appendCostLimitHit(ctx, in.RunID, e.qualID(step.ID), err)
		_ = e.Store.UpsertRunStep(ctx, state.RunStep{
			RunID:      in.RunID,
			StepID:     e.qualID(step.ID),
			Status:     "failed",
			StartedAt:  started,
			FinishedAt: &finished,
			InputJSON:  string(inJSON),
			ErrorText:  err.Error(),
			CostUSD:    stepCost,
		})
		if e.Trace != nil {
			_, _ = e.Trace.Append(ctx, in.RunID, e.qualID(step.ID), trace.EventRunError, trace.ActorSystem, runErrorTraceData(step.ID, err))
		}
		return fmt.Errorf("engine: step %q: %w", step.ID, err)
	}

	if rt.ictx.Steps == nil {
		rt.ictx.Steps = map[string]StepResult{}
	}
	var durationMs int64
	if started != nil {
		durationMs = finished.Sub(*started).Milliseconds()
	}
	rt.ictx.Steps[step.ID] = StepResult{
		Output: out,
		Meta:   map[string]any{"costUsd": stepCost, "durationMs": durationMs},
	}
	rt.completed[step.ID] = struct{}{}

	cpStatus := state.CheckpointStatusRunning
	if rt.interrupting {
		cpStatus = state.CheckpointStatusInterrupted
	}
	if err := e.saveCheckpoint(ctx, wf, in.RunID, i, step.ID, rt.ictx, total, cpStatus); err != nil {
		return fmt.Errorf("engine: checkpoint step %q: %w", step.ID, err)
	}
	outJSON, _ := json.Marshal(out)
	if err := e.Store.UpsertRunStep(ctx, state.RunStep{
		RunID:      in.RunID,
		StepID:     e.qualID(step.ID),
		Status:     "succeeded",
		StartedAt:  started,
		FinishedAt: &finished,
		InputJSON:  string(inJSON),
		OutputJSON: string(outJSON),
		CostUSD:    stepCost,
	}); err != nil {
		return fmt.Errorf("engine: upsert step %q: %w", step.ID, err)
	}
	return nil
}

func (e *Executor) executeOneStep(
	ctx context.Context,
	persistCtx context.Context,
	in RunInput,
	wf *spec.WorkflowResource,
	wfPol policy.PolicyEvaluator,
	rt *dagRuntime,
	ictx Context,
	runStartedAt time.Time,
	runHandle *telemetry.RunHandle,
	i int,
	step spec.WorkflowStep,
) (out map[string]any, stepCost float64, started *time.Time, inJSON []byte, pendingCleared bool, interrupted bool, err error) {
	if strings.TrimSpace(step.ID) == "" {
		return nil, 0, nil, nil, false, false, fmt.Errorf("engine: workflow step missing id")
	}
	if _, err := qualifyStepID(e.stepPrefix, step.ID); err != nil {
		return nil, 0, nil, nil, false, false, err
	}
	uses := strings.TrimSpace(step.Uses)
	agentName := strings.TrimSpace(step.Agent)
	callee := strings.TrimSpace(step.Workflow)
	approval := spec.StepIsApproval(step)
	forms := 0
	if uses != "" {
		forms++
	}
	if agentName != "" {
		forms++
	}
	if callee != "" {
		forms++
	}
	if approval {
		forms++
	}
	if forms != 1 {
		return nil, 0, nil, nil, false, false, fmt.Errorf("engine: step %q must set exactly one of uses, agent, workflow, or approval", step.ID)
	}

	withAny, err := InterpolateWalk(step.With, ictx)
	if err != nil {
		return nil, 0, nil, nil, false, false, fmt.Errorf("engine: step %q with: %w", step.ID, err)
	}
	with, ok := withAny.(map[string]any)
	if !ok {
		with = map[string]any{}
	}

	elapsed := e.now().Sub(runStartedAt)
	pctx := policy.RunContext{
		StartedAt:       runStartedAt,
		Elapsed:         elapsed,
		ApprovedActions: append([]string(nil), in.ApprovedActions...),
	}
	rt.mu.Lock()
	if rt.failed != nil || rt.interrupting {
		rt.mu.Unlock()
		return nil, 0, nil, nil, false, false, context.Canceled
	}
	pctx.AccumulatedCostUSD = rt.cost.get()
	admitErr := wfPol.CheckRun(ctx, pctx)
	rt.mu.Unlock()
	if admitErr != nil {
		e.appendCostLimitHit(persistCtx, in.RunID, e.qualID(step.ID), admitErr)
		inJSON, _ = json.Marshal(with)
		now := e.now()
		return nil, 0, &now, inJSON, false, false, admitErr
	}

	inJSON, _ = json.Marshal(with)
	st := e.now()
	started = &st
	if err := e.Store.UpsertRunStep(persistCtx, state.RunStep{
		RunID:     in.RunID,
		StepID:    e.qualID(step.ID),
		Status:    "running",
		StartedAt: started,
		InputJSON: string(inJSON),
	}); err != nil {
		return nil, 0, started, inJSON, false, false, fmt.Errorf("engine: upsert step %q: %w", step.ID, err)
	}

	if callee != "" {
		out, stepCost, interrupted, err = e.runSubworkflowStep(ctx, persistCtx, in, wf, wfPol, rt, ictx, runStartedAt, runHandle, i, step, with)
		return out, stepCost, started, inJSON, pendingCleared, interrupted, err
	}
	if approval {
		out, pendingCleared, interrupted, err = e.runApprovalStep(ctx, persistCtx, in, wf, wfPol, rt, ictx, pctx, runHandle, i, step, with)
		return out, 0, started, inJSON, pendingCleared, interrupted, err
	}
	if uses != "" {
		toolUses := uses
		toolWith := with
		pending := ictx.PendingHitl
		if pending != nil && pending.StepID != step.ID {
			pending = nil
		}
		if pending == nil {
			rt.mu.Lock()
			liveTotal := rt.cost.get()
			interruptedHITL, ierr := e.maybeInterruptForHitl(persistCtx, in, wf, i, step, with, wfPol, pctx, &rt.ictx, liveTotal, runHandle)
			if interruptedHITL {
				rt.mu.Unlock()
				return nil, 0, started, inJSON, false, true, ierr
			}
			rt.mu.Unlock()
			if in.Hitl.AutoApprove {
				gate, gerr := policy.BuildHitlGateWithEvaluator(e.Graph, wfPol, policySpecFromEvaluator(wfPol), policy.ToolCallContext{
					Run: pctx, StepID: step.ID, Uses: uses, With: with,
				})
				if gerr != nil {
					err = gerr
				} else if gate != nil {
					e.recordAutoApproveHitl(persistCtx, in.RunID, step, i, *gate, in.Hitl.Actor)
					pctx.ApprovedActions = append(append([]string(nil), pctx.ApprovedActions...), uses)
				}
			}
		} else {
			var rerr error
			toolUses, toolWith, rerr = e.resolvePendingHitl(ctx, in, step, wfPol, pctx, pending)
			if rerr != nil {
				err = rerr
			} else {
				pendingCleared = true
				pctx.ApprovedActions = append(append([]string(nil), pctx.ApprovedActions...), toolUses)
			}
		}
		if err == nil {
			var meta tools.ToolCallMeta
			out, meta, err = e.runToolStep(ctx, runHandle, wfPol, wf, in.RunID, step, with, pctx, toolUses, toolWith)
			stepCost = meta.CostUSD
		}
	} else {
		ar, ok := e.Graph.Agents[agentName]
		if !ok || ar == nil {
			err = fmt.Errorf("engine: unknown agent %q", agentName)
		} else {
			var gmeta models.GenerateMeta
			out, gmeta, err = e.runAgentStep(ctx, runHandle, wfPol, wf, in.RunID, step, with, pctx, ar)
			stepCost = gmeta.CostUSD
		}
	}
	if err != nil {
		return out, stepCost, started, inJSON, pendingCleared, false, err
	}
	return out, stepCost, started, inJSON, pendingCleared, false, nil
}
