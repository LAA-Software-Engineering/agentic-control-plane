package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/engine"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/project"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
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
	root := strings.TrimSpace(r.ProjectRoot)
	if root == "" {
		return "", fmt.Errorf("local: empty project root")
	}

	graph, err := project.LoadProject(root)
	if err != nil {
		return "", fmt.Errorf("local: load project: %w", err)
	}
	spec.NormalizeProjectGraph(graph)
	graph, err = ApplyEnvironment(graph, opts.EnvironmentName)
	if err != nil {
		return "", err
	}
	if err := spec.ValidateProjectGraph(graph, root); err != nil {
		return "", fmt.Errorf("local: validate project: %w", err)
	}

	wfName := strings.TrimSpace(opts.WorkflowName)
	if wfName == "" {
		return "", fmt.Errorf("local: empty workflow name")
	}
	wf, ok := graph.Workflows[wfName]
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

	if err := engine.ValidateWorkflowInput(root, wf, input); err != nil {
		return "", err
	}

	if n := spec.TraceRetentionDays(graph); n > 0 {
		cutoff := r.now().UTC().AddDate(0, 0, -n)
		if _, err := r.Store.DeleteRunsStartedBefore(ctx, cutoff); err != nil {
			return "", fmt.Errorf("local: prune trace runs: %w", err)
		}
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
		RunID:        runID,
		WorkflowName: wfName,
		Env:          envLabel,
		Status:       "running",
		StartedAt:    started,
		InputJSON:    string(inputBytes),
		TotalCostUSD: 0,
	}); err != nil {
		return runID, fmt.Errorf("local: start run: %w", err)
	}

	rec := trace.NewRecorder(r.Store)
	if _, err := rec.Append(ctx, runID, "", trace.EventRunStarted, map[string]any{
		"workflow": wfName, "environment": opts.EnvironmentName,
	}); err != nil {
		return runID, fmt.Errorf("local: trace run.started: %w", err)
	}

	ex := &engine.Executor{
		Graph:       graph,
		ProjectRoot: root,
		Tools:       tools.NewRegistry(graph),
		Models:      models.NewRegistry(graph),
		Store:       r.Store,
		Trace:       rec,
		Now:         r.Now,
	}
	runErr := ex.Run(ctx, engine.RunInput{
		RunID:           runID,
		WorkflowName:    wfName,
		Env:             envLabel,
		StartedAt:       started,
		Input:           input,
		ApprovedActions: opts.ApprovedActions,
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
	case "running", "interrupted":
	default:
		return runID, fmt.Errorf("local: run %q status %q is not resumable", runID, run.Status)
	}

	if _, err := r.Store.GetLatestCheckpoint(ctx, runID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return runID, fmt.Errorf("local: run %q has no checkpoint", runID)
		}
		return runID, fmt.Errorf("local: load checkpoint: %w", err)
	}

	root := strings.TrimSpace(r.ProjectRoot)
	if root == "" {
		return runID, fmt.Errorf("local: empty project root")
	}
	graph, err := project.LoadProject(root)
	if err != nil {
		return runID, fmt.Errorf("local: load project: %w", err)
	}
	spec.NormalizeProjectGraph(graph)
	graph, err = ApplyEnvironment(graph, opts.EnvironmentName)
	if err != nil {
		return runID, err
	}
	if err := spec.ValidateProjectGraph(graph, root); err != nil {
		return runID, fmt.Errorf("local: validate project: %w", err)
	}

	wfName := strings.TrimSpace(run.WorkflowName)
	wf, ok := graph.Workflows[wfName]
	if !ok || wf == nil {
		return runID, fmt.Errorf("local: unknown workflow %q", wfName)
	}

	var input map[string]any
	if err := json.Unmarshal([]byte(run.InputJSON), &input); err != nil {
		return runID, fmt.Errorf("local: invalid stored input JSON: %w", err)
	}
	if input == nil {
		input = map[string]any{}
	}
	if err := engine.ValidateWorkflowInput(root, wf, input); err != nil {
		return runID, err
	}

	if n := spec.TraceRetentionDays(graph); n > 0 {
		cutoff := r.now().UTC().AddDate(0, 0, -n)
		if _, err := r.Store.DeleteRunsStartedBefore(ctx, cutoff); err != nil {
			return runID, fmt.Errorf("local: prune trace runs: %w", err)
		}
	}

	if err := r.Store.UpdateRunStatus(ctx, runID, "running"); err != nil {
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

	ex := &engine.Executor{
		Graph:       graph,
		ProjectRoot: root,
		Tools:       tools.NewRegistry(graph),
		Models:      models.NewRegistry(graph),
		Store:       r.Store,
		Trace:       rec,
		Now:         r.Now,
	}
	runErr := ex.Run(ctx, engine.RunInput{
		RunID:           runID,
		WorkflowName:    wfName,
		Env:             envLabel,
		StartedAt:       run.StartedAt,
		Input:           input,
		ApprovedActions: opts.ApprovedActions,
		Resume:          true,
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
