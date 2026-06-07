package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/telemetry"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

// ErrInterrupted is returned when a run pauses at an approval gate or stub interrupt (issue #105).
// Callers should treat this as a clean exit, not a failure.
var ErrInterrupted = errors.New("engine: run interrupted")

const (
	checkpointPayloadVersion  = 1
	maxCheckpointContextBytes = 4 << 20 // absolute load cap (anti-DoS); write path uses resolved limits (#117)
	maxCheckpointSteps        = 256
)

// checkpointPayload is the engine-owned snapshot stored in run_checkpoints.context_json.
type checkpointPayload struct {
	Version       int                   `json:"version"`
	Input         map[string]any        `json:"input"`
	Steps         map[string]StepResult `json:"steps"`
	TotalCostUSD  float64               `json:"totalCostUsd"`
	PendingHitl   *PendingHitlState     `json:"pendingHitl,omitempty"`
	OtelInterrupt *telemetry.SpanRef    `json:"otelInterrupt,omitempty"`
}

func marshalCheckpointPayload(ictx Context, totalCost float64) (string, error) {
	payload := checkpointPayload{
		Version:       checkpointPayloadVersion,
		Input:         ictx.Input,
		Steps:         ictx.Steps,
		TotalCostUSD:  totalCost,
		PendingHitl:   ictx.PendingHitl,
		OtelInterrupt: ictx.OtelInterrupt,
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
	if len(b) > maxCheckpointContextBytes {
		return "", fmt.Errorf("engine: checkpoint context exceeds absolute maximum %d bytes", maxCheckpointContextBytes)
	}
	return string(b), nil
}

func unmarshalCheckpointPayload(contextJSON string, wf *spec.WorkflowResource, completedStepIndex int) (Context, float64, error) {
	if len(contextJSON) > maxCheckpointContextBytes {
		return Context{}, 0, fmt.Errorf("engine: checkpoint context exceeds %d bytes", maxCheckpointContextBytes)
	}
	var payload checkpointPayload
	if err := json.Unmarshal([]byte(contextJSON), &payload); err != nil {
		return Context{}, 0, fmt.Errorf("engine: unmarshal checkpoint: %w", err)
	}
	if payload.Version != checkpointPayloadVersion {
		return Context{}, 0, fmt.Errorf("engine: unsupported checkpoint version %d", payload.Version)
	}
	if payload.Input == nil {
		payload.Input = map[string]any{}
	}
	if payload.Steps == nil {
		payload.Steps = map[string]StepResult{}
	}
	if len(payload.Steps) > maxCheckpointSteps {
		return Context{}, 0, fmt.Errorf("engine: checkpoint has too many steps (%d)", len(payload.Steps))
	}
	if err := validateCheckpointSteps(payload.Steps, wf, completedStepIndex); err != nil {
		return Context{}, 0, err
	}
	if payload.TotalCostUSD < 0 {
		return Context{}, 0, fmt.Errorf("engine: negative totalCostUsd in checkpoint")
	}
	return Context{
		Input: payload.Input, Steps: payload.Steps,
		PendingHitl: payload.PendingHitl, OtelInterrupt: payload.OtelInterrupt,
	}, payload.TotalCostUSD, nil
}

func validateCheckpointSteps(steps map[string]StepResult, wf *spec.WorkflowResource, completedStepIndex int) error {
	if wf == nil {
		return fmt.Errorf("engine: nil workflow for checkpoint validation")
	}
	allowed := make(map[string]struct{}, completedStepIndex+1)
	for i := 0; i <= completedStepIndex && i < len(wf.Spec.Steps); i++ {
		id := strings.TrimSpace(wf.Spec.Steps[i].ID)
		if id != "" {
			allowed[id] = struct{}{}
		}
	}
	for stepID := range steps {
		if _, ok := allowed[stepID]; !ok {
			return fmt.Errorf("engine: checkpoint references unknown or future step %q", stepID)
		}
	}
	return nil
}

func (e *Executor) saveCheckpoint(ctx context.Context, wf *spec.WorkflowResource, runID string, stepIndex int, stepID string, ictx Context, totalCost float64, status string) error {
	ctxJSON, err := marshalCheckpointPayload(ictx, totalCost)
	if err != nil {
		return err
	}
	if err := e.enforceCheckpointSize(ctx, wf, runID, stepID, ctxJSON); err != nil {
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

func (e *Executor) loadResumeState(ctx context.Context, in RunInput) (Context, float64, int, error) {
	cp, err := e.Store.GetLatestCheckpoint(ctx, in.RunID)
	if err != nil {
		return Context{}, 0, 0, fmt.Errorf("engine: load checkpoint: %w", err)
	}
	switch cp.Status {
	case state.CheckpointStatusRunning, state.CheckpointStatusInterrupted:
	default:
		return Context{}, 0, 0, fmt.Errorf("engine: checkpoint status %q is not resumable", cp.Status)
	}
	wf, err := lookupWorkflow(e.Graph, in.WorkflowName)
	if err != nil {
		return Context{}, 0, 0, err
	}
	ictx, totalCost, err := unmarshalCheckpointPayload(cp.ContextJSON, wf, cp.StepIndex)
	if err != nil {
		return Context{}, 0, 0, err
	}
	startIdx := cp.StepIndex + 1
	if ictx.PendingHitl != nil {
		startIdx = cp.StepIndex
	}
	return ictx, totalCost, startIdx, nil
}

func (e *Executor) interruptRun(ctx context.Context, wf *spec.WorkflowResource, in RunInput, stepIndex int, stepID string, ictx Context, totalCost float64, runHandle *telemetry.RunHandle) error {
	if runHandle != nil {
		runHandle.MarkInterrupted()
		ref := runHandle.SpanRef()
		ictx.OtelInterrupt = &ref
	}
	if err := e.saveCheckpoint(ctx, wf, in.RunID, stepIndex, stepID, ictx, totalCost, state.CheckpointStatusInterrupted); err != nil {
		return fmt.Errorf("engine: save interrupted checkpoint: %w", err)
	}
	if err := e.Store.UpdateRunStatus(ctx, in.RunID, state.RunStatusInterrupted); err != nil {
		return fmt.Errorf("engine: mark run interrupted: %w", err)
	}
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, in.RunID, stepID, trace.EventRunError, trace.ActorSystem, map[string]any{
			"stepIndex": stepIndex, "stepId": stepID, "interrupted": true,
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
