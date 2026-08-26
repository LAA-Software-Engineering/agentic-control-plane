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

// Executor runs workflow steps as a DAG (design doc section 12.2 E, section 13, issue #192).
// Workflows with no `needs:` keep implicit sequential YAML order.
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

	stepPrefix string
	rootWF     *spec.WorkflowResource
	nestParent *nestParent
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
	// completing the step at this YAML index. Used to simulate approval gates until HITL lands.
	InterruptAfterStepIndex *int
	// InterruptAfterStepID checkpoints and returns [ErrInterrupted] after that step succeeds.
	// Used to pause a parallel group after one branch completes (issue #192).
	InterruptAfterStepID string
	// MaxConcurrentSteps bounds goroutine fan-out (issue #192). Zero uses DefaultMaxConcurrentSteps.
	MaxConcurrentSteps int
	// WorkflowDepth is 0 for the entry run and increments for each workflow: step (issue #194).
	WorkflowDepth int
	// CallStack is callee workflow names from the root (issue #194 traces).
	CallStack []string
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

// Run executes a workflow: interpolate step inputs, policy checks, tool/agent calls,
// optional JSON Schema validation for agent output, persisted run_steps and trace events.
// Independent steps (graph mode via `needs:`) run concurrently with a concurrency bound;
// workflows with no `needs:` keep YAML-order sequential execution (issue #192).
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

	wfPol, err := compiledWorkflowEvaluator(e.ProjectRoot, e.Graph, strings.TrimSpace(wf.Spec.Policy))
	if err != nil {
		return e.failRun(ctx, in, err, 0)
	}

	ictx := Context{Input: in.Input, Steps: make(map[string]StepResult)}
	var totalCost float64
	completed := map[string]struct{}{}
	if in.Resume {
		ictx, totalCost, completed, err = e.loadResumeState(ctx, in)
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

	ictx, totalCost, err = e.runWorkflowSteps(ctx, in, wf, wfPol, ictx, nil, totalCost, completed, runStartedAt, runHandle)
	if err != nil {
		return err
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
		_, _ = e.Trace.Append(ctx, in.RunID, stepID, trace.EventRunError, trace.ActorSystem, runErrorTraceData(stepID, runErr))
	}
	return e.failRun(ctx, in, runErr, totalCost)
}

func runErrorTraceData(stepID string, err error) map[string]any {
	data := map[string]any{"stepId": stepID}
	if err != nil {
		data["error"] = err.Error()
	}
	if d, ok := policy.AsDenied(err); ok {
		data["reason"] = d.Reason
	}
	return data
}
