package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/telemetry"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

// subworkflowRunID derives the deterministic child run id for a `workflow:` step so a resumed
// parent re-enters and resumes the same child run rather than starting a fresh one (issue #194).
func subworkflowRunID(parentRunID, stepID string) string {
	return strings.TrimSpace(parentRunID) + "::" + strings.TrimSpace(stepID)
}

// runSubworkflowStep invokes the callee workflow as a nested child run and returns its
// output.value as the step output (issue #194). The child run carries ParentRunID for trace
// nesting and executes under its own resolved Policy; the caller's Policy authority ceiling is
// enforced statically as the union of caller and callee effects (see docs §7.4).
//
// When the child interrupts (HITL / stub interrupt), the parent run is checkpointed as
// interrupted from live state under rt.mu and ErrInterrupted is propagated so a later resume
// re-enters this step and resumes the same child run. A child that already succeeded (parent
// crashed between child completion and the parent commit) is not re-run; its output is reused.
func (e *Executor) runSubworkflowStep(
	ctx, persistCtx context.Context,
	in RunInput,
	wf *spec.WorkflowResource,
	stepIndex int,
	step spec.WorkflowStep,
	calleeName string,
	input map[string]any,
	rt *dagRuntime,
	runHandle *telemetry.RunHandle,
) (out map[string]any, cost float64, interrupted bool, err error) {
	depth := in.SubworkflowDepth + 1
	if depth > spec.DefaultMaxSubworkflowDepth {
		return nil, 0, false, fmt.Errorf("engine: subworkflow %q exceeds nesting depth limit %d", calleeName, spec.DefaultMaxSubworkflowDepth)
	}
	if _, lookupErr := lookupWorkflow(e.Graph, calleeName); lookupErr != nil {
		return nil, 0, false, lookupErr
	}
	if input == nil {
		input = map[string]any{}
	}

	childRunID := subworkflowRunID(in.RunID, step.ID)
	existing, getErr := e.Store.GetRun(persistCtx, childRunID)
	if getErr != nil && !errors.Is(getErr, sql.ErrNoRows) {
		return nil, 0, false, fmt.Errorf("engine: subworkflow %q: get child run: %w", calleeName, getErr)
	}

	resume := false
	if existing != nil {
		switch existing.Status {
		case state.RunStatusSucceeded:
			out, err = decodeChildOutput(existing.OutputJSON)
			return out, existing.TotalCostUSD, false, err
		case state.RunStatusFailed:
			return nil, existing.TotalCostUSD, false, fmt.Errorf("engine: subworkflow %q previously failed: %s", calleeName, existing.ErrorText)
		default:
			resume = true
		}
	} else if err := e.startChildRun(persistCtx, in, calleeName, childRunID, input); err != nil {
		return nil, 0, false, err
	}

	childErr := e.Run(ctx, RunInput{
		RunID:                childRunID,
		WorkflowName:         calleeName,
		Env:                  in.Env,
		StartedAt:            e.now(),
		Input:                input,
		ApprovedActions:      append([]string(nil), in.ApprovedActions...),
		Resume:               resume,
		Hitl:                 in.Hitl,
		MaxConcurrentSteps:   in.MaxConcurrentSteps,
		InterruptAfterStepID: in.InterruptAfterStepID,
		SubworkflowDepth:     depth,
		TenantID:             in.TenantID,
		ThreadID:             in.ThreadID,
		ActorID:              in.ActorID,
		RequestID:            in.RequestID,
	})

	child, err := e.Store.GetRun(persistCtx, childRunID)
	if err != nil {
		if childErr != nil {
			return nil, 0, false, fmt.Errorf("engine: subworkflow %q: %w", calleeName, childErr)
		}
		return nil, 0, false, fmt.Errorf("engine: subworkflow %q: load child run: %w", calleeName, err)
	}

	if errors.Is(childErr, ErrInterrupted) {
		rt.mu.Lock()
		ierr := e.interruptRun(persistCtx, wf, in, stepIndex, step.ID, rt.ictx, rt.totalCost, runHandle)
		rt.mu.Unlock()
		return nil, child.TotalCostUSD, true, ierr
	}
	if childErr != nil {
		return nil, child.TotalCostUSD, false, fmt.Errorf("engine: subworkflow %q: %w", calleeName, childErr)
	}

	out, err = decodeChildOutput(child.OutputJSON)
	if err != nil {
		return nil, child.TotalCostUSD, false, fmt.Errorf("engine: subworkflow %q: %w", calleeName, err)
	}
	return out, child.TotalCostUSD, false, nil
}

// startChildRun inserts a fresh child run row (with ParentRunID for trace nesting) and emits a
// nested run_started event.
func (e *Executor) startChildRun(ctx context.Context, in RunInput, calleeName, childRunID string, input map[string]any) error {
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("engine: subworkflow %q: marshal input: %w", calleeName, err)
	}
	if err := e.Store.StartRun(ctx, state.Run{
		RunID:        childRunID,
		WorkflowName: calleeName,
		Env:          in.Env,
		Status:       state.RunStatusRunning,
		StartedAt:    e.now(),
		InputJSON:    string(inputBytes),
		TenantID:     in.TenantID,
		ThreadID:     in.ThreadID,
		ActorID:      in.ActorID,
		ParentRunID:  in.RunID,
		RequestID:    in.RequestID,
	}); err != nil {
		return fmt.Errorf("engine: subworkflow %q: start child run: %w", calleeName, err)
	}
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, childRunID, "", trace.EventRunStarted, trace.ActorAgent, map[string]any{
			"workflow":    calleeName,
			"parentRunId": in.RunID,
		})
	}
	return nil
}

func decodeChildOutput(outputJSON string) (map[string]any, error) {
	outputJSON = strings.TrimSpace(outputJSON)
	if outputJSON == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(outputJSON), &out); err != nil {
		return nil, fmt.Errorf("child output is not a JSON object: %w", err)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}
