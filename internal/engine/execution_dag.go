package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/telemetry"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

// DefaultMaxConcurrentSteps bounds workflow goroutine fan-out (issue #192).
const DefaultMaxConcurrentSteps = 8

type dagRuntime struct {
	mu           sync.Mutex
	completed    map[string]struct{}
	running      map[int]struct{}
	failed       error
	interrupting bool
	interruptErr error
	totalCost    float64
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
	totalCost float64,
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
	steps := wf.Spec.Steps
	rt := &dagRuntime{
		completed: completed,
		running:   map[int]struct{}{},
		totalCost: totalCost,
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
				e.runDAGStep(ctx, cancel, rt, in, wf, wfPol, runStartedAt, runHandle, i, completion)
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
		stuck := nRunning == 0 && !allDone && failed == nil && !interrupting
		rt.mu.Unlock()
		if failed != nil || interrupting || allDone {
			break
		}
		if stuck {
			wg.Wait()
			return rt.ictx, rt.totalCost, e.failRun(parent, in, fmt.Errorf("engine: workflow graph has no runnable step"), rt.totalCost)
		}
		<-completion
		trySchedule()
	}
	cancel()
	wg.Wait()

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.failed != nil {
		return rt.ictx, rt.totalCost, e.failRun(parent, in, rt.failed, rt.totalCost)
	}
	if rt.interrupting {
		if rt.interruptErr != nil {
			return rt.ictx, rt.totalCost, rt.interruptErr
		}
		return rt.ictx, rt.totalCost, ErrInterrupted
	}
	if !workflowFullyComplete(steps, rt.completed) {
		return rt.ictx, rt.totalCost, e.failRun(parent, in, fmt.Errorf("engine: workflow graph incomplete after scheduling"), rt.totalCost)
	}
	return rt.ictx, rt.totalCost, nil
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
	}
	cost := rt.totalCost
	rt.mu.Unlock()

	out, stepCost, started, inJSON, pendingCleared, interrupted, err := ex.executeOneStep(ctx, in, wf, wfPol, rt, snap, cost, runStartedAt, runHandle, i, step)

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
			_ = ex.Store.UpsertRunStep(ctx, state.RunStep{
				RunID:      in.RunID,
				StepID:     step.ID,
				Status:     "failed",
				StartedAt:  started,
				FinishedAt: &finished,
				InputJSON:  string(inJSON),
				ErrorText:  err.Error(),
				CostUSD:    stepCost,
			})
			if ex.Trace != nil && !isContextCanceled(err) {
				_, _ = ex.Trace.Append(ctx, in.RunID, step.ID, trace.EventRunError, trace.ActorSystem, runErrorTraceData(step.ID, err))
			}
			if strings.Contains(err.Error(), "engine:") {
				rt.failed = err
			} else {
				rt.failed = fmt.Errorf("engine: step %q: %w", step.ID, err)
			}
			rt.totalCost += stepCost
		}
		cancel()
		return
	}
	if rt.interrupting || rt.failed != nil {
		return
	}

	finished := ex.now()
	rt.totalCost += stepCost
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

	// Checkpoint the merged completion set before the succeeded row (issue #105 / #192).
	if err := ex.saveCheckpoint(ctx, wf, in.RunID, i, step.ID, rt.ictx, rt.totalCost, state.CheckpointStatusRunning); err != nil {
		rt.failed = fmt.Errorf("engine: checkpoint step %q: %w", step.ID, err)
		cancel()
		return
	}
	outJSON, _ := json.Marshal(out)
	if err := ex.Store.UpsertRunStep(ctx, state.RunStep{
		RunID:      in.RunID,
		StepID:     step.ID,
		Status:     "succeeded",
		StartedAt:  started,
		FinishedAt: &finished,
		InputJSON:  string(inJSON),
		OutputJSON: string(outJSON),
		CostUSD:    stepCost,
	}); err != nil {
		rt.failed = fmt.Errorf("engine: upsert step %q: %w", step.ID, err)
		cancel()
		return
	}

	if in.InterruptAfterStepIndex != nil && i == *in.InterruptAfterStepIndex {
		rt.interrupting = true
		rt.interruptErr = ex.interruptRun(ctx, wf, in, i, step.ID, rt.ictx, rt.totalCost, runHandle)
		cancel()
		return
	}
	if id := strings.TrimSpace(in.InterruptAfterStepID); id != "" && id == step.ID {
		rt.interrupting = true
		rt.interruptErr = ex.interruptRun(ctx, wf, in, i, step.ID, rt.ictx, rt.totalCost, runHandle)
		cancel()
		return
	}
}

func (e *Executor) executeOneStep(
	ctx context.Context,
	in RunInput,
	wf *spec.WorkflowResource,
	wfPol policy.PolicyEvaluator,
	rt *dagRuntime,
	ictx Context,
	totalCost float64,
	runStartedAt time.Time,
	runHandle *telemetry.RunHandle,
	i int,
	step spec.WorkflowStep,
) (out map[string]any, stepCost float64, started *time.Time, inJSON []byte, pendingCleared bool, interrupted bool, err error) {
	if strings.TrimSpace(step.ID) == "" {
		return nil, 0, nil, nil, false, false, fmt.Errorf("engine: workflow step missing id")
	}
	uses := strings.TrimSpace(step.Uses)
	agentName := strings.TrimSpace(step.Agent)
	if (uses == "") == (agentName == "") {
		return nil, 0, nil, nil, false, false, fmt.Errorf("engine: step %q must set exactly one of uses or agent", step.ID)
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
		StartedAt:          runStartedAt,
		Elapsed:            elapsed,
		AccumulatedCostUSD: totalCost,
		ApprovedActions:    append([]string(nil), in.ApprovedActions...),
	}
	if err := wfPol.CheckRun(ctx, pctx); err != nil {
		e.appendCostLimitHit(ctx, in.RunID, step.ID, err)
		inJSON, _ = json.Marshal(with)
		now := e.now()
		return nil, 0, &now, inJSON, false, false, err
	}

	inJSON, _ = json.Marshal(with)
	st := e.now()
	started = &st
	if err := e.Store.UpsertRunStep(ctx, state.RunStep{
		RunID:     in.RunID,
		StepID:    step.ID,
		Status:    "running",
		StartedAt: started,
		InputJSON: string(inJSON),
	}); err != nil {
		return nil, 0, started, inJSON, false, false, fmt.Errorf("engine: upsert step %q: %w", step.ID, err)
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
			live := rt.ictx
			liveCost := rt.totalCost
			interruptedHITL, ierr := e.maybeInterruptForHitl(ctx, in, wf, i, step, with, wfPol, pctx, live, liveCost, runHandle)
			if interruptedHITL {
				rt.ictx.PendingHitl = live.PendingHitl
				rt.ictx.OtelInterrupt = live.OtelInterrupt
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
					e.recordAutoApproveHitl(ctx, in.RunID, step, i, *gate, in.Hitl.Actor)
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
