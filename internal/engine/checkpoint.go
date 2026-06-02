package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

// ErrInterrupted is returned when a run pauses at an approval gate or stub interrupt (issue #105).
// Callers should treat this as a clean exit, not a failure.
var ErrInterrupted = errors.New("engine: run interrupted")

// checkpointPayload is the engine-owned snapshot stored in run_checkpoints.context_json.
type checkpointPayload struct {
	Input        map[string]any        `json:"input"`
	Steps        map[string]StepResult `json:"steps"`
	TotalCostUSD float64               `json:"totalCostUsd"`
}

func marshalCheckpointPayload(ictx Context, totalCost float64) (string, error) {
	payload := checkpointPayload{
		Input:        ictx.Input,
		Steps:        ictx.Steps,
		TotalCostUSD: totalCost,
	}
	if payload.Input == nil {
		payload.Input = map[string]any{}
	}
	if payload.Steps == nil {
		payload.Steps = map[string]StepResult{}
	}
	b, err := render.MarshalStableJSON(payload)
	if err != nil {
		return "", fmt.Errorf("engine: marshal checkpoint: %w", err)
	}
	return string(b), nil
}

func unmarshalCheckpointPayload(contextJSON string) (Context, float64, error) {
	var payload checkpointPayload
	if err := json.Unmarshal([]byte(contextJSON), &payload); err != nil {
		return Context{}, 0, fmt.Errorf("engine: unmarshal checkpoint: %w", err)
	}
	if payload.Input == nil {
		payload.Input = map[string]any{}
	}
	if payload.Steps == nil {
		payload.Steps = map[string]StepResult{}
	}
	return Context{Input: payload.Input, Steps: payload.Steps}, payload.TotalCostUSD, nil
}

func (e *Executor) saveCheckpoint(ctx context.Context, runID string, stepIndex int, stepID string, ictx Context, totalCost float64, status string) error {
	ctxJSON, err := marshalCheckpointPayload(ictx, totalCost)
	if err != nil {
		return err
	}
	return e.Store.SaveCheckpoint(ctx, state.RunCheckpoint{
		RunID:       runID,
		StepIndex:   stepIndex,
		StepID:      stepID,
		ContextJSON: ctxJSON,
		Status:      status,
		CreatedAt:   e.now(),
	})
}

func (e *Executor) loadResumeState(ctx context.Context, runID string) (Context, float64, int, error) {
	cp, err := e.Store.GetLatestCheckpoint(ctx, runID)
	if err != nil {
		return Context{}, 0, 0, fmt.Errorf("engine: load checkpoint: %w", err)
	}
	switch cp.Status {
	case state.CheckpointStatusRunning, state.CheckpointStatusInterrupted:
	default:
		return Context{}, 0, 0, fmt.Errorf("engine: checkpoint status %q is not resumable", cp.Status)
	}
	ictx, totalCost, err := unmarshalCheckpointPayload(cp.ContextJSON)
	if err != nil {
		return Context{}, 0, 0, err
	}
	return ictx, totalCost, cp.StepIndex + 1, nil
}

func (e *Executor) interruptRun(ctx context.Context, in RunInput, stepIndex int, stepID string, ictx Context, totalCost float64) error {
	if err := e.saveCheckpoint(ctx, in.RunID, stepIndex, stepID, ictx, totalCost, state.CheckpointStatusInterrupted); err != nil {
		return fmt.Errorf("engine: save interrupted checkpoint: %w", err)
	}
	if err := e.Store.UpdateRunStatus(ctx, in.RunID, "interrupted"); err != nil {
		return fmt.Errorf("engine: mark run interrupted: %w", err)
	}
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, in.RunID, stepID, trace.EventRunInterrupted, map[string]any{
			"stepIndex": stepIndex, "stepId": stepID,
		})
	}
	return ErrInterrupted
}

// resumeRunStartedAt returns StartedAt for resumed runs, using the original run row when available.
func resumeRunStartedAt(ctx context.Context, store state.RuntimeStore, in RunInput) time.Time {
	if !in.Resume || store == nil {
		return in.StartedAt
	}
	run, err := store.GetRun(ctx, in.RunID)
	if err != nil || run == nil {
		return in.StartedAt
	}
	return run.StartedAt
}
