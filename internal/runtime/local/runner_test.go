package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/engine"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/project"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

func testRunProjRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "runproj")
}

func testRetentionProjRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "retention")
}

func TestExecuteWorkflow_persistsRunAndTraceInSQLite(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "localrun.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	rt := NewRuntime(testRunProjRoot(t), st)
	runID := "run-integration-1"
	_, err = rt.ExecuteWorkflow(ctx, runtime.WorkflowRunOptions{
		RunID:           runID,
		WorkflowName:    "demo",
		EnvironmentName: "staging",
		Env:             "dev",
		InputJSON:       []byte(`{"topic":"from-local-runtime"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "succeeded" || got.ErrorText != "" {
		t.Fatalf("run %+v", got)
	}

	events, err := trace.NewReader(st).ListByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 {
		t.Fatalf("want trace events, got %d", len(events))
	}
	if events[0].Type != trace.EventRunStarted {
		t.Fatalf("first event %q", events[0].Type)
	}
	if events[len(events)-1].Type != trace.EventRunFinished {
		t.Fatalf("last event %q", events[len(events)-1].Type)
	}
}

func TestExecuteWorkflow_invalidInputJSON_noRunRow(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "norun.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	rt := NewRuntime(testRunProjRoot(t), st)
	_, err = rt.ExecuteWorkflow(ctx, runtime.WorkflowRunOptions{
		RunID:        "should-not-exist",
		WorkflowName: "demo",
		InputJSON:    []byte(`{"topic":`),
	})
	if err == nil {
		t.Fatal("expected error")
	}

	_, err = st.GetRun(ctx, "should-not-exist")
	if err == nil {
		t.Fatal("expected no run row")
	}
}

func TestExecuteWorkflow_invalidInputSchema_noRunRow(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "norun2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	rt := NewRuntime(testRunProjRoot(t), st)
	_, err = rt.ExecuteWorkflow(ctx, runtime.WorkflowRunOptions{
		RunID:        "schema-fail",
		WorkflowName: "demo",
		InputJSON:    []byte(`{"wrong":true}`),
	})
	if err == nil {
		t.Fatal("expected schema validation error")
	}

	_, err = st.GetRun(ctx, "schema-fail")
	if err == nil {
		t.Fatal("expected no run row")
	}
}

func TestApplyEnvironment_mergesAgentConstraints(t *testing.T) {
	g, err := project.LoadProject(testRunProjRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	out, err := ApplyEnvironment(g, "staging")
	if err != nil {
		t.Fatal(err)
	}
	a := out.Agents["reviewer"]
	if a == nil || a.Spec.Constraints == nil || a.Spec.Constraints.TimeoutSeconds != 99 {
		t.Fatalf("constraints %+v", a)
	}
}

func TestNewRunID_generatedWhenEmpty(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "genid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	rt := NewRuntime(testRunProjRoot(t), st)
	id, err := rt.ExecuteWorkflow(ctx, runtime.WorkflowRunOptions{
		WorkflowName: "demo",
		InputJSON:    []byte(`{"topic":"x"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty run id")
	}
	_, err = st.GetRun(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
}

func TestExecuteWorkflow_prunesOldTraceRuns(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "retention.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	fixed := time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)
	oldID := "stale-run"
	oldStart := fixed.Add(-72 * time.Hour)
	if err := st.StartRun(ctx, state.Run{
		RunID: oldID, WorkflowName: "demo", Env: "local", Status: "succeeded",
		StartedAt: oldStart, InputJSON: `{}`, TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTraceEvent(ctx, oldID, oldStart, trace.EventRunStarted, "", `{}`); err != nil {
		t.Fatal(err)
	}

	rt := NewRuntime(testRetentionProjRoot(t), st)
	rt.Now = func() time.Time { return fixed }

	newID := "fresh-run"
	_, err = rt.ExecuteWorkflow(ctx, runtime.WorkflowRunOptions{
		RunID:           newID,
		WorkflowName:    "demo",
		EnvironmentName: "staging",
		Env:             "dev",
		InputJSON:       []byte(`{"topic":"p"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.GetRun(ctx, oldID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("old run: %v", err)
	}
	if _, err := st.GetRun(ctx, newID); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteWorkflow_resumeAfterInterrupt(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "resume-local.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := testRunProjRoot(t)
	graph, err := project.LoadProject(root)
	if err != nil {
		t.Fatal(err)
	}
	spec.NormalizeProjectGraph(graph)
	graph, err = ApplyEnvironment(graph, "staging")
	if err != nil {
		t.Fatal(err)
	}

	runID := "resume-local-1"
	started := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	inputJSON := []byte(`{"topic":"resume-me"}`)
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "demo", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: string(inputJSON), TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}

	var input map[string]any
	if err := json.Unmarshal(inputJSON, &input); err != nil {
		t.Fatal(err)
	}
	idx := 0
	ex := &engine.Executor{
		Graph: graph, ProjectRoot: root,
		Tools: tools.NewRegistry(graph), Models: models.NewRegistry(graph),
		Store: st, Trace: trace.NewRecorder(st),
		Now: func() time.Time { return started },
	}
	if err := ex.Run(ctx, engine.RunInput{
		RunID: runID, WorkflowName: "demo", Env: "dev", StartedAt: started, Input: input,
		InterruptAfterStepIndex: &idx,
	}); !errors.Is(err, engine.ErrInterrupted) {
		t.Fatalf("interrupt: %v", err)
	}

	rt := NewRuntime(root, st)
	rt.Now = func() time.Time { return started.Add(time.Hour) }
	if _, err := rt.ExecuteWorkflow(ctx, runtime.WorkflowRunOptions{
		RunID: runID, Resume: true, EnvironmentName: "staging",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "succeeded" {
		t.Fatalf("status %q err=%q", got.Status, got.ErrorText)
	}

	events, err := trace.NewReader(st).ListByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	var resumed, fetchStarts int
	for _, ev := range events {
		if ev.Type == trace.EventRunResumed {
			resumed++
		}
		if ev.StepID == "fetch" && ev.Type == trace.EventStepStarted {
			fetchStarts++
		}
	}
	if resumed != 1 {
		t.Fatalf("run.resumed count = %d", resumed)
	}
	if fetchStarts != 1 {
		t.Fatalf("fetch step.started count = %d want 1", fetchStarts)
	}
}
