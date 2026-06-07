package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/engine"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/telemetry"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/util"
)

// Invoke validates input, persists the run row, and executes the workflow engine.
func (r *Runtime) Invoke(ctx context.Context, cfg *config.ResolvedConfig, opts runtime.InvokeOptions) (runtime.RunResult, error) {
	if r == nil || r.Store == nil {
		return runtime.RunResult{}, fmt.Errorf("local: nil runtime or store")
	}
	prep, err := r.prepareFromConfig(ctx, cfg)
	if err != nil {
		return runtime.RunResult{}, err
	}

	wfName := strings.TrimSpace(opts.WorkflowName)
	if wfName == "" {
		return runtime.RunResult{}, fmt.Errorf("local: empty workflow name")
	}
	wf, ok := prep.graph.Workflows[wfName]
	if !ok || wf == nil {
		return runtime.RunResult{}, fmt.Errorf("local: unknown workflow %q", wfName)
	}

	var input map[string]any
	if len(opts.InputJSON) == 0 {
		input = map[string]any{}
	} else {
		if err := json.Unmarshal(opts.InputJSON, &input); err != nil {
			return runtime.RunResult{}, fmt.Errorf("local: invalid input JSON: %w", err)
		}
	}
	if err := engine.ValidateWorkflowInput(prep.root, wf, input); err != nil {
		return runtime.RunResult{}, err
	}

	wfHash, err := plan.WorkflowSpecHash(wf)
	if err != nil {
		return runtime.RunResult{}, err
	}

	runID := strings.TrimSpace(opts.RunID)
	if runID == "" {
		runID = util.NewRunID()
	}

	inputBytes, err := json.Marshal(input)
	if err != nil {
		return runtime.RunResult{}, fmt.Errorf("local: marshal input: %w", err)
	}

	envLabel := strings.TrimSpace(opts.Env)
	if envLabel == "" {
		envLabel = "local"
	}

	started := r.now()
	attr := state.RunAttribution{
		TenantID:       opts.TenantID,
		ThreadID:       opts.ThreadID,
		ActorID:        opts.ActorID,
		ParentRunID:    opts.ParentRunID,
		RequestID:      opts.RequestID,
		IdempotencyKey: opts.IdempotencyKey,
		Source:         opts.Source,
	}
	if opts.RequireAttribution {
		if err := state.RequireExplicitAttribution(attr); err != nil {
			return runtime.RunResult{RunID: runID}, err
		}
	}
	runRow := state.Run{
		RunID:            runID,
		WorkflowName:     wfName,
		Env:              envLabel,
		Status:           state.RunStatusRunning,
		StartedAt:        started,
		InputJSON:        string(inputBytes),
		TotalCostUSD:     0,
		WorkflowSpecHash: wfHash,
		EnvironmentName:  strings.TrimSpace(opts.EnvironmentName),
	}
	state.ApplyAttribution(&runRow, attr)
	if err := r.Store.StartRun(ctx, runRow); err != nil {
		return runtime.RunResult{RunID: runID}, fmt.Errorf("local: start run: %w", err)
	}

	rec := trace.NewRecorderForGraph(r.Store, prep.graph)
	if _, err := rec.Append(ctx, runID, "", trace.EventRunStarted, map[string]any{
		"workflow": wfName, "environment": cfg.Environment(),
	}); err != nil {
		return runtime.RunResult{RunID: runID}, fmt.Errorf("local: trace run.started: %w", err)
	}

	runCfg := engineRunConfigFromInvoke(opts)
	_, runErr := r.executeEngine(ctx, prep, runID, wfName, envLabel, started, input, runCfg, false, state.AttributionFromRun(&runRow), rec)
	return runtime.RunResult{RunID: runID}, runErr
}

// Resume continues an existing run from its latest checkpoint.
func (r *Runtime) Resume(ctx context.Context, cfg *config.ResolvedConfig, opts runtime.ResumeOptions) (runtime.RunResult, error) {
	if r == nil || r.Store == nil {
		return runtime.RunResult{}, fmt.Errorf("local: nil runtime or store")
	}
	runID := strings.TrimSpace(opts.RunID)
	if runID == "" {
		return runtime.RunResult{}, fmt.Errorf("local: resume requires run id")
	}

	run, err := r.Store.GetRun(ctx, runID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return runtime.RunResult{RunID: runID}, fmt.Errorf("local: run %q not found", runID)
		}
		return runtime.RunResult{RunID: runID}, fmt.Errorf("local: get run: %w", err)
	}
	switch run.Status {
	case state.RunStatusRunning, state.RunStatusInterrupted:
	default:
		return runtime.RunResult{RunID: runID}, fmt.Errorf("local: run %q status %q is not resumable", runID, run.Status)
	}

	if _, err := r.Store.GetLatestCheckpoint(ctx, runID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return runtime.RunResult{RunID: runID}, fmt.Errorf("local: run %q has no checkpoint", runID)
		}
		return runtime.RunResult{RunID: runID}, fmt.Errorf("local: load checkpoint: %w", err)
	}

	if _, err := resolveConfigForResume(run, opts.EnvironmentName); err != nil {
		return runtime.RunResult{RunID: runID}, err
	}

	prep, err := r.prepareFromConfig(ctx, cfg)
	if err != nil {
		return runtime.RunResult{RunID: runID}, err
	}

	wfName := strings.TrimSpace(run.WorkflowName)
	wf, ok := prep.graph.Workflows[wfName]
	if !ok || wf == nil {
		return runtime.RunResult{RunID: runID}, fmt.Errorf("local: unknown workflow %q", wfName)
	}
	if err := validateResumeWorkflowSpec(run, wf); err != nil {
		return runtime.RunResult{RunID: runID}, err
	}

	var input map[string]any
	if err := json.Unmarshal([]byte(run.InputJSON), &input); err != nil {
		return runtime.RunResult{RunID: runID}, fmt.Errorf("local: invalid stored input JSON: %w", err)
	}
	if input == nil {
		input = map[string]any{}
	}
	if err := engine.ValidateWorkflowInput(prep.root, wf, input); err != nil {
		return runtime.RunResult{RunID: runID}, err
	}

	if err := r.Store.UpdateRunStatus(ctx, runID, state.RunStatusRunning); err != nil {
		return runtime.RunResult{RunID: runID}, fmt.Errorf("local: mark run running: %w", err)
	}

	rec := trace.NewRecorderForGraph(r.Store, prep.graph)
	if _, err := rec.Append(ctx, runID, "", trace.EventRunResumed, map[string]any{
		"workflow": wfName,
	}); err != nil {
		return runtime.RunResult{RunID: runID}, fmt.Errorf("local: trace run.resumed: %w", err)
	}

	envLabel := strings.TrimSpace(run.Env)
	if envLabel == "" {
		envLabel = "local"
	}

	runCfg := engineRunConfigFromResume(opts)
	_, runErr := r.executeEngine(ctx, prep, runID, wfName, envLabel, run.StartedAt, input, runCfg, true, state.AttributionFromRun(run), rec)
	return runtime.RunResult{RunID: runID}, runErr
}

func (r *Runtime) executeEngine(
	ctx context.Context,
	prep *preparedProject,
	runID, wfName, envLabel string,
	started time.Time,
	input map[string]any,
	cfg engineRunConfig,
	resume bool,
	attr state.RunAttribution,
	rec *trace.Recorder,
) (string, error) {
	tel := telemetry.NewTracer(telemetry.ConfigFromGraph(prep.graph), r.agentVersion())
	defer tel.Shutdown()

	ex := &engine.Executor{
		Graph:       prep.graph,
		ProjectRoot: prep.root,
		Tools:       tools.NewRegistry(prep.graph),
		Models:      models.NewRegistry(prep.graph),
		Store:       r.Store,
		Trace:       rec,
		Telemetry:   tel,
		Now:         r.Now,
	}
	hitl, err := buildEngineHitlOptions(cfg)
	if err != nil {
		return runID, err
	}
	state.NormalizeAttribution(&attr)

	runErr := ex.Run(ctx, engine.RunInput{
		RunID:           runID,
		WorkflowName:    wfName,
		Env:             envLabel,
		StartedAt:       started,
		Input:           input,
		ApprovedActions: cfg.approvedActions,
		Resume:          resume,
		Hitl:            hitl,
		TenantID:        attr.TenantID,
		ThreadID:        attr.ThreadID,
		ActorID:         attr.ActorID,
		RequestID:       attr.RequestID,
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
