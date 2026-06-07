package engine

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

func graphWithLimits(t *testing.T, projectLimits, toolLimits *spec.ExecutionLimits) *spec.ProjectGraph {
	t.Helper()
	g := echoOnlyWorkflow(t)
	g.Spec.Limits = projectLimits
	if tr := g.Tools["helper"]; tr != nil && toolLimits != nil {
		tr.Spec.Limits = toolLimits
	}
	return g
}

func echoOnlyWorkflow(t *testing.T) *spec.ProjectGraph {
	t.Helper()
	g := demoWorkflowGraph(t)
	g.Workflows["demo"].Spec.Steps = []spec.WorkflowStep{{
		ID:   "fetch",
		Uses: "tool.helper.echo",
		With: map[string]any{
			"topic": "${input.topic}",
			"extra": strings.Repeat("x", 500),
		},
	}}
	g.Workflows["demo"].Spec.Output = &spec.WorkflowOutput{
		Value: map[string]any{"topic": "${input.topic}"},
	}
	return g
}

func startDemoRun(t *testing.T, st state.RuntimeStore, runID string, started time.Time) map[string]any {
	t.Helper()
	inJSON := `{"topic":"agents"}`
	if err := st.StartRun(context.Background(), state.Run{
		RunID: runID, WorkflowName: "demo", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: inJSON,
	}); err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(inJSON), &input); err != nil {
		t.Fatal(err)
	}
	return input
}

func TestRun_toolOutputTruncatedAtLimit(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "limits.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := graphWithLimits(t, &spec.ExecutionLimits{
		MaxToolOutputBytes:     80,
		ToolOutputExceedPolicy: spec.LimitExceedTruncate,
	}, nil)

	runID := "run-truncate"
	started := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	input := startDemoRun(t, st, runID, started)

	ex := &Executor{
		Graph:       graph,
		ProjectRoot: testProjectRoot(t),
		Tools:       tools.NewRegistry(graph),
		Store:       st,
		Trace:       trace.NewRecorder(st),
	}
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "demo", Env: "dev", StartedAt: started, Input: input,
		Hitl: HitlRunOptions{AutoApprove: true},
	}); err != nil {
		t.Fatal(err)
	}

	events, err := st.ListTraceEventsByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	var limitHits int
	for _, ev := range events {
		if ev.Type == trace.EventLimitHit.String() {
			limitHits++
			if !strings.Contains(ev.DataJSON, `"kind":"tool_output"`) {
				t.Fatalf("unexpected limit_hit payload: %s", ev.DataJSON)
			}
			if !strings.Contains(ev.DataJSON, `"truncated":true`) {
				t.Fatalf("expected truncated annotation: %s", ev.DataJSON)
			}
		}
	}
	if limitHits == 0 {
		t.Fatal("expected limit_hit trace event")
	}
}

func TestRun_toolOutputFailPolicy(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "limits-fail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := graphWithLimits(t, &spec.ExecutionLimits{
		MaxToolOutputBytes:     80,
		ToolOutputExceedPolicy: spec.LimitExceedFail,
	}, nil)

	runID := "run-fail"
	started := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	input := startDemoRun(t, st, runID, started)

	ex := &Executor{
		Graph:       graph,
		ProjectRoot: testProjectRoot(t),
		Tools:       tools.NewRegistry(graph),
		Store:       st,
		Trace:       trace.NewRecorder(st),
	}
	err = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "demo", Env: "dev", StartedAt: started, Input: input,
		Hitl: HitlRunOptions{AutoApprove: true},
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error(), "tool_output exceeds limit") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_toolOutputAtBoundary(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "limits-boundary.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := demoWorkflowGraph(t)
	graph.Workflows["demo"].Spec.Steps = []spec.WorkflowStep{{
		ID: "fetch", Uses: "tool.helper.echo",
		With: map[string]any{"payload": strings.Repeat("a", 20)},
	}}
	graph.Workflows["demo"].Spec.Output = &spec.WorkflowOutput{
		Value: map[string]any{"out": "${steps.fetch.output.echo}"},
	}

	payload := map[string]any{"payload": strings.Repeat("a", 20)}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	limit := len(b) + 40
	graph.Spec.Limits = &spec.ExecutionLimits{
		MaxToolOutputBytes:     limit,
		ToolOutputExceedPolicy: spec.LimitExceedFail,
	}

	runID := "run-boundary"
	started := time.Date(2026, 6, 7, 13, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "demo", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}

	ex := &Executor{
		Graph: graph, ProjectRoot: testProjectRoot(t),
		Tools: tools.NewRegistry(graph), Store: st, Trace: trace.NewRecorder(st),
	}
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "demo", Env: "dev", StartedAt: started,
		Input: map[string]any{}, Hitl: HitlRunOptions{AutoApprove: true},
	}); err != nil {
		t.Fatalf("boundary should pass: %v", err)
	}
}

func TestRun_checkpointExceedsLimitFailsClosed(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "limits-cp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := echoOnlyWorkflow(t)
	graph.Spec.Limits = &spec.ExecutionLimits{MaxCheckpointBytes: 200}

	runID := "run-cp"
	started := time.Date(2026, 6, 7, 14, 0, 0, 0, time.UTC)
	input := startDemoRun(t, st, runID, started)

	ex := &Executor{
		Graph: graph, ProjectRoot: testProjectRoot(t),
		Tools: tools.NewRegistry(graph), Store: st, Trace: trace.NewRecorder(st),
	}
	err = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "demo", Env: "dev", StartedAt: started, Input: input,
		Hitl: HitlRunOptions{AutoApprove: true},
	})
	if err == nil {
		t.Fatal("expected checkpoint limit failure")
	}
	if !strings.Contains(err.Error(), "checkpoint context exceeds") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveToolLimits_toolOverrideWins(t *testing.T) {
	graph := graphWithLimits(t, &spec.ExecutionLimits{MaxToolInputBytes: 1000}, &spec.ExecutionLimits{MaxToolInputBytes: 100})
	ex := &Executor{Graph: graph}
	wf := graph.Workflows["demo"]
	got := ex.resolveToolLimits(wf, "tool.helper.echo")
	if got.MaxToolInputBytes != 100 {
		t.Fatalf("tool override = %d", got.MaxToolInputBytes)
	}
}

func TestEnforceMapLimit_concurrentSafety(t *testing.T) {
	t.Parallel()
	ex := &Executor{Trace: nil}
	ctx := context.Background()
	v := map[string]any{"x": strings.Repeat("z", 100)}
	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			_, err := ex.enforceMapLimit(ctx, "r", "s", "tool.helper.echo", spec.LimitKindToolInput, v, 1000, spec.LimitExceedTruncate)
			done <- err
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
