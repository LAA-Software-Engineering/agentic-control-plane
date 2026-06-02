package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/engine"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/util"
)

// Compile-time check that [Runtime] implements [runtime.WorkflowRunner].
var _ runtime.WorkflowRunner = (*Runtime)(nil)

// ExecuteWorkflow loads the project from [Runtime.ProjectRoot], applies optional environment overrides,
// validates input JSON and workflow input schema before persisting the run, then invokes [engine.Executor].
// When opts.Resume is true, the existing run row and latest checkpoint are rehydrated instead of StartRun.
func (r *Runtime) ExecuteWorkflow(ctx context.Context, opts runtime.WorkflowRunOptions) (string, error) {
	if r == nil || r.Store == nil {
		return "", fmt.Errorf("local: nil runtime or store")
	}
	if opts.Resume {
		return r.resumeWorkflow(ctx, opts)
	}
	return r.startWorkflow(ctx, opts)
}

func (r *Runtime) startWorkflow(ctx context.Context, opts runtime.WorkflowRunOptions) (string, error) {
	prep, err := r.prepareProject(ctx, opts.EnvironmentName)
	if err != nil {
		return "", err
	}

	wfName := strings.TrimSpace(opts.WorkflowName)
	if wfName == "" {
		return "", fmt.Errorf("local: empty workflow name")
	}
	wf, ok := prep.graph.Workflows[wfName]
	if !ok || wf == nil {
		return "", fmt.Errorf("local: unknown workflow %q", wfName)
	}

	var input map[string]any
	if len(opts.InputJSON) == 0 {
		input = map[string]any{}
	} else {
		if err := json.Unmarshal(opts.InputJSON, &input); err != nil {
			return "", fmt.Errorf("local: invalid input JSON: %w", err)
		}
	}
	if err := engine.ValidateWorkflowInput(prep.root, wf, input); err != nil {
		return "", err
	}

	wfHash, err := plan.WorkflowSpecHash(wf)
	if err != nil {
		return "", err
	}

	runID := strings.TrimSpace(opts.RunID)
	if runID == "" {
		runID = util.NewRunID()
	}

	inputBytes, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("local: marshal input: %w", err)
	}

	envLabel := strings.TrimSpace(opts.Env)
	if envLabel == "" {
		envLabel = "local"
	}

	started := r.now()
	if err := r.Store.StartRun(ctx, state.Run{
		RunID:            runID,
		WorkflowName:     wfName,
		Env:              envLabel,
		Status:           state.RunStatusRunning,
		StartedAt:        started,
		InputJSON:        string(inputBytes),
		TotalCostUSD:     0,
		WorkflowSpecHash: wfHash,
		EnvironmentName:  strings.TrimSpace(opts.EnvironmentName),
	}); err != nil {
		return runID, fmt.Errorf("local: start run: %w", err)
	}

	rec := trace.NewRecorder(r.Store)
	if _, err := rec.Append(ctx, runID, "", trace.EventRunStarted, map[string]any{
		"workflow": wfName, "environment": opts.EnvironmentName,
	}); err != nil {
		return runID, fmt.Errorf("local: trace run.started: %w", err)
	}

	opts.RunID = runID
	opts.Resume = false
	return r.executeEngine(ctx, prep, runID, wfName, envLabel, started, input, opts, rec)
}

func (r *Runtime) resumeWorkflow(ctx context.Context, opts runtime.WorkflowRunOptions) (string, error) {
	runID := strings.TrimSpace(opts.RunID)
	if runID == "" {
		return "", fmt.Errorf("local: resume requires run id")
	}

	run, err := r.Store.GetRun(ctx, runID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return runID, fmt.Errorf("local: run %q not found", runID)
		}
		return runID, fmt.Errorf("local: get run: %w", err)
	}
	switch run.Status {
	case state.RunStatusRunning, state.RunStatusInterrupted:
	default:
		return runID, fmt.Errorf("local: run %q status %q is not resumable", runID, run.Status)
	}

	if _, err := r.Store.GetLatestCheckpoint(ctx, runID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return runID, fmt.Errorf("local: run %q has no checkpoint", runID)
		}
		return runID, fmt.Errorf("local: load checkpoint: %w", err)
	}

	envName, err := resumeEnvironmentName(run, opts)
	if err != nil {
		return runID, err
	}

	prep, err := r.prepareProject(ctx, envName)
	if err != nil {
		return runID, err
	}

	wfName := strings.TrimSpace(run.WorkflowName)
	wf, ok := prep.graph.Workflows[wfName]
	if !ok || wf == nil {
		return runID, fmt.Errorf("local: unknown workflow %q", wfName)
	}
	if err := validateResumeWorkflowSpec(run, wf); err != nil {
		return runID, err
	}

	var input map[string]any
	if err := json.Unmarshal([]byte(run.InputJSON), &input); err != nil {
		return runID, fmt.Errorf("local: invalid stored input JSON: %w", err)
	}
	if input == nil {
		input = map[string]any{}
	}
	if err := engine.ValidateWorkflowInput(prep.root, wf, input); err != nil {
		return runID, err
	}

	if err := r.Store.UpdateRunStatus(ctx, runID, state.RunStatusRunning); err != nil {
		return runID, fmt.Errorf("local: mark run running: %w", err)
	}

	rec := trace.NewRecorder(r.Store)
	if _, err := rec.Append(ctx, runID, "", trace.EventRunResumed, map[string]any{
		"workflow": wfName,
	}); err != nil {
		return runID, fmt.Errorf("local: trace run.resumed: %w", err)
	}

	envLabel := strings.TrimSpace(run.Env)
	if envLabel == "" {
		envLabel = "local"
	}

	opts.Resume = true
	return r.executeEngine(ctx, prep, runID, wfName, envLabel, run.StartedAt, input, opts, rec)
}

func (r *Runtime) executeEngine(
	ctx context.Context,
	prep *preparedProject,
	runID, wfName, envLabel string,
	started time.Time,
	input map[string]any,
	opts runtime.WorkflowRunOptions,
	rec *trace.Recorder,
) (string, error) {
	ex := &engine.Executor{
		Graph:       prep.graph,
		ProjectRoot: prep.root,
		Tools:       tools.NewRegistry(prep.graph),
		Models:      models.NewRegistry(prep.graph),
		Store:       r.Store,
		Trace:       rec,
		Now:         r.Now,
	}
	hitl, err := buildEngineHitlOptions(opts)
	if err != nil {
		return runID, err
	}
	runErr := ex.Run(ctx, engine.RunInput{
		RunID:           runID,
		WorkflowName:    wfName,
		Env:             envLabel,
		StartedAt:       started,
		Input:           input,
		ApprovedActions: opts.ApprovedActions,
		Resume:          opts.Resume,
		Hitl:            hitl,
	})

	finData := map[string]any{}
	if runErr != nil {
		if errors.Is(runErr, engine.ErrInterrupted) {
			return runID, nil
		}
		finData["error"] = runErr.Error()
	}
	if _, terr := rec.Append(ctx, runID, "", trace.EventRunFinished, finData); terr != nil && runErr == nil {
		return runID, fmt.Errorf("local: trace run.finished: %w", terr)
	}
	return runID, runErr
}
