package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/telemetry"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

// Executor runs sequential workflow steps (design doc section 12.2 E, section 13).
type Executor struct {
	Graph       *spec.ProjectGraph
	ProjectRoot string
	Tools       tools.ToolExecutor
	Models      *models.Registry
	// ModelResolve, if set, is used instead of Models.ClientFor (tests inject mocks).
	ModelResolve func(modelRef string) (models.ModelClient, string, error)
	Store        state.RuntimeStore
	Trace        *trace.Recorder
	Telemetry    *telemetry.Tracer
	Now          func() time.Time
}

// RunInput identifies the workflow run and parsed input map (already JSON-valid).
type RunInput struct {
	RunID           string
	WorkflowName    string
	Env             string
	StartedAt       time.Time
	Input           map[string]any
	ApprovedActions []string
	// Resume loads the latest checkpoint and continues from the next step (issue #105).
	Resume bool
	// Hitl carries operator decisions for approval gates (issue #106).
	Hitl HitlRunOptions
	// InterruptAfterStepIndex, when non-nil, checkpoints and returns [ErrInterrupted] after
	// completing the step at this index. Used to simulate approval gates until HITL lands.
	InterruptAfterStepIndex *int
	// Attribution for OTel gen_ai attributes (issue #111).
	TenantID  string
	ThreadID  string
	ActorID   string
	RequestID string
}

func (e *Executor) now() time.Time {
	if e != nil && e.Now != nil {
		return e.Now()
	}
	return time.Now().UTC()
}

func (e *Executor) modelClient(modelRef string) (models.ModelClient, string, error) {
	if e.ModelResolve != nil {
		return e.ModelResolve(modelRef)
	}
	if e.Models == nil {
		return nil, "", fmt.Errorf("engine: Models registry is nil")
	}
	return e.Models.ClientFor(modelRef)
}

// Run executes a workflow sequentially: interpolate step inputs, policy checks, tool/agent calls,
// optional JSON Schema validation for agent output, persisted run_steps and trace events.
// The run row must already exist in [state.RuntimeStore] (e.g. via [state.RuntimeStore.StartRun]).
func (e *Executor) Run(ctx context.Context, in RunInput) (err error) {
	if e == nil || e.Store == nil {
		return fmt.Errorf("engine: nil executor or store")
	}
	if e.Graph == nil {
		return fmt.Errorf("engine: nil project graph")
	}
	wf, err := lookupWorkflow(e.Graph, in.WorkflowName)
	if err != nil {
		return err
	}
	if err := validateWorkflowInput(e.ProjectRoot, wf, in.Input); err != nil {
		return e.failRun(ctx, in, err, 0)
	}

	polEng := policy.NewEngine(e.Graph)
	wfPol := polEng.Evaluator(strings.TrimSpace(wf.Spec.Policy))

	ictx := Context{Input: in.Input, Steps: make(map[string]StepResult)}
	var totalCost float64
	stepStartIdx := 0
	if in.Resume {
		ictx, totalCost, stepStartIdx, err = e.loadResumeState(ctx, in)
		if err != nil {
			return err
		}
	}

	var runHandle *telemetry.RunHandle
	if e.Telemetry != nil && e.Telemetry.Enabled() {
		var link *telemetry.SpanRef
		if in.Resume {
			link = ictx.OtelInterrupt
		}
		actorID := strings.TrimSpace(in.ActorID)
		if actorID == "" {
			actorID = strings.TrimSpace(in.Hitl.Actor)
		}
		runHandle = e.Telemetry.BeginRun(ctx, telemetry.RunStartAttrs{
			RunID:     in.RunID,
			Workflow:  in.WorkflowName,
			AgentName: primaryAgentName(wf),
			TenantID:  in.TenantID,
			ThreadID:  in.ThreadID,
			ActorID:   actorID,
			RequestID: in.RequestID,
			Resume:    in.Resume,
			LinkFrom:  link,
		})
		if runHandle != nil {
			ctx = runHandle.Context()
		}
	}
	defer func() {
		if runHandle == nil {
			return
		}
		if errors.Is(err, ErrInterrupted) {
			runHandle.EndInterrupted()
			return
		}
		runHandle.End(err)
	}()

	runStartedAt := resumeRunStartedAt(ctx, e.Store, in)
	finishAt := e.now()

	for i, step := range wf.Spec.Steps {
		if i < stepStartIdx {
			continue
		}
		step := step
		if strings.TrimSpace(step.ID) == "" {
			return e.failRun(ctx, in, fmt.Errorf("engine: workflow step missing id"), totalCost)
		}
		uses := strings.TrimSpace(step.Uses)
		agentName := strings.TrimSpace(step.Agent)
		if (uses == "") == (agentName == "") {
			return e.failRun(ctx, in, fmt.Errorf("engine: step %q must set exactly one of uses or agent", step.ID), totalCost)
		}

		withAny, err := InterpolateWalk(step.With, ictx)
		if err != nil {
			return e.failRun(ctx, in, fmt.Errorf("engine: step %q with: %w", step.ID, err), totalCost)
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
			ApprovedActions:    in.ApprovedActions,
		}
		if err := wfPol.CheckRun(ctx, pctx); err != nil {
			return e.failRunStep(ctx, in, step.ID, with, err, totalCost)
		}

		inJSON, _ := json.Marshal(with)
		started := e.now()
		if err := e.Store.UpsertRunStep(ctx, state.RunStep{
			RunID:     in.RunID,
			StepID:    step.ID,
			Status:    "running",
			StartedAt: &started,
			InputJSON: string(inJSON),
		}); err != nil {
			return e.failRun(ctx, in, fmt.Errorf("engine: upsert step %q: %w", step.ID, err), totalCost)
		}

		var out map[string]any
		var stepCost float64
		if uses != "" {
			toolUses := uses
			toolWith := with
			pending := ictx.PendingHitl
			if pending == nil {
				interrupted, ierr := e.maybeInterruptForHitl(ctx, in, wf, i, step, with, wfPol, pctx, ictx, totalCost, runHandle)
				if interrupted {
					return ierr
				}
				if in.Hitl.AutoApprove {
					gate, gerr := policy.BuildHitlGate(e.Graph, policySpecFromEvaluator(wfPol), policy.ToolCallContext{
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
					ictx.PendingHitl = nil
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
				out, gmeta, err = e.runAgentStep(ctx, runHandle, wfPol, in.RunID, step, with, pctx, ar)
				stepCost = gmeta.CostUSD
			}
		}

		finished := e.now()
		totalCost += stepCost
		if err != nil {
			_ = e.Store.UpsertRunStep(ctx, state.RunStep{
				RunID:      in.RunID,
				StepID:     step.ID,
				Status:     "failed",
				StartedAt:  &started,
				FinishedAt: &finished,
				InputJSON:  string(inJSON),
				ErrorText:  err.Error(),
				CostUSD:    stepCost,
			})
			if e.Trace != nil {
				_, _ = e.Trace.Append(ctx, in.RunID, step.ID, trace.EventRunError, trace.ActorSystem, map[string]any{"error": err.Error(), "stepId": step.ID})
			}
			return e.failRun(ctx, in, fmt.Errorf("engine: step %q: %w", step.ID, err), totalCost)
		}

		meta := map[string]any{"costUsd": stepCost, "durationMs": finished.Sub(started).Milliseconds()}
		ictx.Steps[step.ID] = StepResult{Output: out, Meta: meta}

		// Checkpoint before marking the step succeeded so resume never replays a completed step
		// if the process dies after persistence (issue #105 / PR #127).
		if err := e.saveCheckpoint(ctx, wf, in.RunID, i, step.ID, ictx, totalCost, state.CheckpointStatusRunning); err != nil {
			return e.failRun(ctx, in, fmt.Errorf("engine: checkpoint step %q: %w", step.ID, err), totalCost)
		}

		outJSON, _ := json.Marshal(out)
		if err := e.Store.UpsertRunStep(ctx, state.RunStep{
			RunID:      in.RunID,
			StepID:     step.ID,
			Status:     "succeeded",
			StartedAt:  &started,
			FinishedAt: &finished,
			InputJSON:  string(inJSON),
			OutputJSON: string(outJSON),
			CostUSD:    stepCost,
		}); err != nil {
			return e.failRun(ctx, in, fmt.Errorf("engine: upsert step %q: %w", step.ID, err), totalCost)
		}
		if in.InterruptAfterStepIndex != nil && i == *in.InterruptAfterStepIndex {
			return e.interruptRun(ctx, wf, in, i, step.ID, ictx, totalCost, runHandle)
		}
	}

	finalOut, err := buildWorkflowOutput(wf, ictx)
	if err != nil {
		return e.failRun(ctx, in, err, totalCost)
	}
	outBytes, err := json.Marshal(finalOut)
	if err != nil {
		return e.failRun(ctx, in, err, totalCost)
	}
	finishAt = e.now()
	if err := e.saveCheckpoint(ctx, wf, in.RunID, len(wf.Spec.Steps)-1, "", ictx, totalCost, state.CheckpointStatusCompleted); err != nil {
		return e.failRun(ctx, in, fmt.Errorf("engine: final checkpoint: %w", err), totalCost)
	}
	return e.Store.FinishRun(ctx, in.RunID, state.RunStatusSucceeded, finishAt, string(outBytes), "", totalCost)
}

func primaryAgentName(wf *spec.WorkflowResource) string {
	if wf == nil {
		return ""
	}
	for _, step := range wf.Spec.Steps {
		if n := strings.TrimSpace(step.Agent); n != "" {
			return n
		}
	}
	return ""
}

func (e *Executor) failRun(ctx context.Context, in RunInput, runErr error, totalCost float64) error {
	wf, _ := lookupWorkflow(e.Graph, in.WorkflowName)
	ictx := Context{Input: in.Input, Steps: map[string]StepResult{}}
	_ = e.saveCheckpoint(ctx, wf, in.RunID, -1, "", ictx, totalCost, state.CheckpointStatusFailed)
	finishAt := e.now()
	_ = e.Store.FinishRun(ctx, in.RunID, state.RunStatusFailed, finishAt, "", runErr.Error(), totalCost)
	return runErr
}

func (e *Executor) failRunStep(ctx context.Context, in RunInput, stepID string, with map[string]any, runErr error, totalCost float64) error {
	inJSON, _ := json.Marshal(with)
	now := e.now()
	_ = e.Store.UpsertRunStep(ctx, state.RunStep{
		RunID:      in.RunID,
		StepID:     stepID,
		Status:     "failed",
		StartedAt:  &now,
		FinishedAt: &now,
		InputJSON:  string(inJSON),
		ErrorText:  runErr.Error(),
	})
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, in.RunID, stepID, trace.EventRunError, trace.ActorSystem, map[string]any{"error": runErr.Error(), "stepId": stepID})
	}
	return e.failRun(ctx, in, runErr, totalCost)
}
