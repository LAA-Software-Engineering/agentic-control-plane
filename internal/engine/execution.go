package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
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
func (e *Executor) Run(ctx context.Context, in RunInput) error {
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
	finishAt := e.now()

	for _, step := range wf.Spec.Steps {
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

		elapsed := e.now().Sub(in.StartedAt)
		pctx := policy.RunContext{
			StartedAt:          in.StartedAt,
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
		if e.Trace != nil {
			_, _ = e.Trace.Append(ctx, in.RunID, step.ID, trace.EventStepStarted, map[string]any{"uses": uses, "agent": agentName})
		}

		var out map[string]any
		var stepCost float64
		if uses != "" {
			var meta tools.ToolCallMeta
			out, meta, err = e.runToolStep(ctx, wfPol, in.RunID, step, with, pctx)
			stepCost = meta.CostUSD
		} else {
			ar, ok := e.Graph.Agents[agentName]
			if !ok || ar == nil {
				err = fmt.Errorf("engine: unknown agent %q", agentName)
			} else {
				var gmeta models.GenerateMeta
				out, gmeta, err = e.runAgentStep(ctx, wfPol, in.RunID, step, with, pctx, ar)
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
				_, _ = e.Trace.Append(ctx, in.RunID, step.ID, trace.EventStepFailed, map[string]any{"error": err.Error()})
			}
			return e.failRun(ctx, in, fmt.Errorf("engine: step %q: %w", step.ID, err), totalCost)
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
		if e.Trace != nil {
			_, _ = e.Trace.Append(ctx, in.RunID, step.ID, trace.EventStepFinished, map[string]any{"costUsd": stepCost})
		}

		meta := map[string]any{"costUsd": stepCost, "durationMs": finished.Sub(started).Milliseconds()}
		ictx.Steps[step.ID] = StepResult{Output: out, Meta: meta}
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
	return e.Store.FinishRun(ctx, in.RunID, "succeeded", finishAt, string(outBytes), "", totalCost)
}

func (e *Executor) failRun(ctx context.Context, in RunInput, runErr error, totalCost float64) error {
	finishAt := e.now()
	_ = e.Store.FinishRun(ctx, in.RunID, "failed", finishAt, "", runErr.Error(), totalCost)
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
		_, _ = e.Trace.Append(ctx, in.RunID, stepID, trace.EventStepFailed, map[string]any{"error": runErr.Error()})
	}
	return e.failRun(ctx, in, runErr, totalCost)
}
