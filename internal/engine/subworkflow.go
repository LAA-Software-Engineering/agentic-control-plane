package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/terfyn/internal/policy"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/telemetry"
	"github.com/LAA-Software-Engineering/terfyn/internal/trace"
)

type nestParent struct {
	rt     *dagRuntime
	stepID string
	callee string
	parent *nestParent
	rootWF *spec.WorkflowResource
}

func qualifyStepID(prefix, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("engine: empty step id")
	}
	if strings.Contains(id, "/") {
		return "", fmt.Errorf("engine: step id %q must not contain '/'", id)
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return id, nil
	}
	return prefix + "/" + id, nil
}

func (e *Executor) qualID(id string) string {
	prefix := ""
	if e != nil {
		prefix = e.stepPrefix
	}
	out, err := qualifyStepID(prefix, id)
	if err != nil {
		return id
	}
	return out
}

func (e *Executor) wrapNestedCheckpoint(ictx Context) Context {
	if e == nil || e.nestParent == nil {
		return ictx
	}
	return wrapNestStack(e.nestParent, ictx)
}

func wrapNestStack(n *nestParent, inner Context) Context {
	if n == nil {
		return inner
	}
	n.rt.mu.Lock()
	outer := Context{
		Input:         n.rt.ictx.Input,
		Steps:         cloneStepResults(n.rt.ictx.Steps),
		PendingHitl:   n.rt.ictx.PendingHitl,
		OtelInterrupt: n.rt.ictx.OtelInterrupt,
	}
	n.rt.mu.Unlock()
	outer.Nested = &NestedRunState{
		StepID:      n.stepID,
		Workflow:    n.callee,
		Input:       inner.Input,
		Steps:       inner.Steps,
		Completed:   unionCompletedIDs(inner.Steps, nil),
		PendingHitl: inner.PendingHitl,
		Nested:      inner.Nested,
	}
	return wrapNestStack(n.parent, outer)
}

func (e *Executor) runSubworkflowStep(
	ctx context.Context,
	persistCtx context.Context,
	in RunInput,
	wf *spec.WorkflowResource,
	callerPol policy.PolicyEvaluator,
	rt *dagRuntime,
	ictx Context,
	runStartedAt time.Time,
	runHandle *telemetry.RunHandle,
	i int,
	step spec.WorkflowStep,
	with map[string]any,
) (out map[string]any, stepCost float64, interrupted bool, err error) {
	calleeName := strings.TrimSpace(step.Workflow)
	callee, err := lookupWorkflow(e.Graph, calleeName)
	if err != nil {
		return nil, 0, false, fmt.Errorf("engine: step %q: %w", step.ID, err)
	}
	limitWF := wf
	if e.rootWF != nil {
		limitWF = e.rootWF
	}
	maxDepth := spec.ResolveMaxWorkflowNesting(&e.Graph.Spec, &limitWF.Spec)
	depth := in.WorkflowDepth + 1
	if depth > maxDepth {
		return nil, 0, false, fmt.Errorf("engine: step %q: workflow nesting depth %d exceeds maxWorkflowNesting %d", step.ID, depth, maxDepth)
	}
	if err := e.validateWorkflowInputSchema(callee, with); err != nil {
		return nil, 0, false, fmt.Errorf("engine: step %q subworkflow %q input: %w", step.ID, calleeName, err)
	}

	calleePol, err := compiledWorkflowEvaluator(e.ProjectRoot, e.Graph, strings.TrimSpace(callee.Spec.Policy), e.PinnedGraph)
	if err != nil {
		return nil, 0, false, err
	}
	wfPol := policy.StricterOf(callerPol, calleePol)

	child := *e
	prefix, err := qualifyStepID(e.stepPrefix, step.ID)
	if err != nil {
		return nil, 0, false, err
	}
	child.stepPrefix = prefix
	if e.rootWF != nil {
		child.rootWF = e.rootWF
	} else {
		child.rootWF = wf
	}
	child.nestParent = &nestParent{
		rt:     rt,
		stepID: step.ID,
		callee: calleeName,
		parent: e.nestParent,
		rootWF: child.rootWF,
	}
	if child.Trace != nil {
		stack := append(append([]string(nil), in.CallStack...), calleeName)
		child.Trace = child.Trace.WithCallStack(stack)
	}

	if e.Trace != nil {
		_, _ = e.Trace.Append(persistCtx, in.RunID, e.qualID(step.ID), trace.EventWorkflowCallStarted, trace.ActorSystem, map[string]any{
			"workflow": calleeName,
			"depth":    depth,
			"stepId":   step.ID,
		})
	}

	childIn := in
	childIn.WorkflowName = calleeName
	childIn.WorkflowDepth = depth
	childIn.CallStack = append(append([]string(nil), in.CallStack...), calleeName)
	childIn.Resume = false

	childIctx := Context{Input: with, Steps: map[string]StepResult{}}
	completed := map[string]struct{}{}
	if ictx.Nested != nil && strings.TrimSpace(ictx.Nested.StepID) == strings.TrimSpace(step.ID) {
		ns := ictx.Nested
		if strings.TrimSpace(ns.Workflow) == calleeName {
			childIctx.Input = ns.Input
			if childIctx.Input == nil {
				childIctx.Input = with
			}
			childIctx.Steps = ns.Steps
			if childIctx.Steps == nil {
				childIctx.Steps = map[string]StepResult{}
			}
			childIctx.PendingHitl = ns.PendingHitl
			childIctx.Nested = ns.Nested
			for _, id := range ns.Completed {
				id = strings.TrimSpace(id)
				if id != "" {
					completed[id] = struct{}{}
				}
			}
			for id := range childIctx.Steps {
				completed[id] = struct{}{}
			}
		}
	}

	startCost := rt.cost.get()
	childIctx, total, err := child.runWorkflowSteps(ctx, childIn, callee, wfPol, childIctx, rt.cost, startCost, completed, runStartedAt, runHandle)
	stepCost = total - startCost
	if err != nil {
		if errorsIsInterrupted(err) {
			rt.mu.Lock()
			rt.ictx.Nested = &NestedRunState{
				StepID:      step.ID,
				Workflow:    calleeName,
				Input:       childIctx.Input,
				Steps:       childIctx.Steps,
				Completed:   unionCompletedIDs(childIctx.Steps, completed),
				PendingHitl: childIctx.PendingHitl,
				Nested:      childIctx.Nested,
			}
			rt.ictx.OtelInterrupt = childIctx.OtelInterrupt
			rt.mu.Unlock()
			return nil, stepCost, true, err
		}
		return nil, stepCost, false, err
	}
	rt.mu.Lock()
	rt.ictx.Nested = nil
	rt.mu.Unlock()
	out, err = buildWorkflowOutput(callee, childIctx)
	if err != nil {
		return nil, stepCost, false, fmt.Errorf("engine: step %q subworkflow %q output: %w", step.ID, calleeName, err)
	}
	if e.Trace != nil {
		_, _ = e.Trace.Append(persistCtx, in.RunID, e.qualID(step.ID), trace.EventWorkflowCallFinished, trace.ActorSystem, map[string]any{
			"workflow": calleeName,
			"depth":    depth,
			"stepId":   step.ID,
		})
	}
	return out, stepCost, false, nil
}

func errorsIsInterrupted(err error) bool {
	return errors.Is(err, ErrInterrupted)
}

func unionCompletedIDs(steps map[string]StepResult, extra map[string]struct{}) []string {
	seen := map[string]struct{}{}
	for id := range steps {
		id = strings.TrimSpace(id)
		if id != "" {
			seen[id] = struct{}{}
		}
	}
	for id := range extra {
		id = strings.TrimSpace(id)
		if id != "" {
			seen[id] = struct{}{}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
