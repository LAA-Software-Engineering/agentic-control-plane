package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
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

func testResolvedConfig(t *testing.T, root, env string) *config.ResolvedConfig {
	t.Helper()
	rc, err := config.Resolve(config.ResolveOptions{ProjectRoot: root, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	return rc
}

func copyTestProject(t *testing.T, src string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "proj")
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("copy test project: %v", err)
	}
	return dst
}

func TestInvoke_persistsRunAndTraceInSQLite(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "localrun.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := testRunProjRoot(t)
	rt := NewRuntime(st)
	rc := testResolvedConfig(t, root, "staging")
	runID := "run-integration-1"
	_, err = rt.Invoke(ctx, rc, runtime.InvokeOptions{
		RunID:        runID,
		WorkflowName: "demo",
		Env:          "dev",
		InputJSON:    []byte(`{"topic":"from-local-runtime"}`),
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

func TestInvoke_invalidInputJSON_noRunRow(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "norun.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := testRunProjRoot(t)
	rt := NewRuntime(st)
	rc := testResolvedConfig(t, root, "")
	_, err = rt.Invoke(ctx, rc, runtime.InvokeOptions{
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

func TestInvoke_invalidInputSchema_noRunRow(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "norun2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := testRunProjRoot(t)
	rt := NewRuntime(st)
	rc := testResolvedConfig(t, root, "")
	_, err = rt.Invoke(ctx, rc, runtime.InvokeOptions{
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

func TestInvoke_usesResolvedSnapshotNotDisk(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "snapshot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := copyTestProject(t, testRunProjRoot(t))
	rc := testResolvedConfig(t, root, "staging")
	projectPath := filepath.Join(root, "project.yaml")
	if err := os.WriteFile(projectPath, []byte("invalid: yaml: ["), 0o644); err != nil {
		t.Fatal(err)
	}

	rt := NewRuntime(st)
	runID := "snapshot-run"
	if _, err := rt.Invoke(ctx, rc, runtime.InvokeOptions{
		RunID:        runID,
		WorkflowName: "demo",
		Env:          "dev",
		InputJSON:    []byte(`{"topic":"snapshot"}`),
	}); err != nil {
		t.Fatalf("invoke should use resolved snapshot, not disk: %v", err)
	}
}

func TestApplyEnvironment_mergesAgentConstraints(t *testing.T) {
	g, err := project.LoadProject(testRunProjRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	out, err := spec.ApplyEnvironment(g, "staging")
	if err != nil {
		t.Fatal(err)
	}
	a := out.Agents["reviewer"]
	if a == nil || a.Spec.Constraints == nil || a.Spec.Constraints.TimeoutSeconds != 99 {
		t.Fatalf("constraints %+v", a)
	}
}

func TestInvoke_generatedRunIDWhenEmpty(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "genid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := testRunProjRoot(t)
	rt := NewRuntime(st)
	rc := testResolvedConfig(t, root, "")
	result, err := rt.Invoke(ctx, rc, runtime.InvokeOptions{
		WorkflowName: "demo",
		InputJSON:    []byte(`{"topic":"x"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunID == "" {
		t.Fatal("empty run id")
	}
	_, err = st.GetRun(ctx, result.RunID)
	if err != nil {
		t.Fatal(err)
	}
}

func TestInvoke_prunesOldTraceRuns(t *testing.T) {
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

	root := testRetentionProjRoot(t)
	rt := NewRuntime(st)
	rt.Now = func() time.Time { return fixed }
	rc := testResolvedConfig(t, root, "staging")

	newID := "fresh-run"
	_, err = rt.Invoke(ctx, rc, runtime.InvokeOptions{
		RunID:        newID,
		WorkflowName: "demo",
		Env:          "dev",
		InputJSON:    []byte(`{"topic":"p"}`),
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

func TestResume_afterInterrupt(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "resume-local.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := testRunProjRoot(t)
	rc := testResolvedConfig(t, root, "staging")
	graph := rc.Graph()

	runID := "resume-local-1"
	started := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	inputJSON := []byte(`{"topic":"resume-me"}`)
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "demo", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: string(inputJSON), TotalCostUSD: 0,
		EnvironmentName: "staging",
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

	rt := NewRuntime(st)
	rt.Now = func() time.Time { return started.Add(time.Hour) }
	if _, err := rt.Resume(ctx, rc, runtime.ResumeOptions{
		RunID: runID, EnvironmentName: "staging",
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

func TestResume_preservesAttribution(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "resume-attr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := testRunProjRoot(t)
	rc := testResolvedConfig(t, root, "staging")
	graph := rc.Graph()

	runID := "resume-attr-1"
	started := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	inputJSON := []byte(`{"topic":"resume-attr"}`)
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "demo", Env: "dev", Status: state.RunStatusRunning,
		StartedAt: started, InputJSON: string(inputJSON), TotalCostUSD: 0,
		TenantID: "acme", ThreadID: "thread-original", ActorID: "starter-bot",
		RequestID: "req-original", Source: "cli",
		EnvironmentName: "staging",
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
		TenantID: "acme", ThreadID: "thread-original", ActorID: "starter-bot", RequestID: "req-original",
		InterruptAfterStepIndex: &idx,
	}); !errors.Is(err, engine.ErrInterrupted) {
		t.Fatalf("interrupt: %v", err)
	}

	rt := NewRuntime(st)
	rt.Now = func() time.Time { return started.Add(time.Hour) }
	if _, err := rt.Resume(ctx, rc, runtime.ResumeOptions{
		RunID: runID, EnvironmentName: "staging",
		TenantID: "other-tenant", ThreadID: "thread-override", ActorID: "other-actor",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "acme" || got.ThreadID != "thread-original" || got.ActorID != "starter-bot" {
		t.Fatalf("run attribution changed: %+v", got)
	}

	events, err := trace.NewReader(st).ListByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Type == trace.EventRunResumed {
			if ev.TenantID != "acme" || ev.ThreadID != "thread-original" || ev.ActorID != "starter-bot" {
				t.Fatalf("resume trace attribution: %+v", ev)
			}
		}
	}
}

func TestHealth_nilStore(t *testing.T) {
	var rt *Runtime
	status := rt.Health(context.Background())
	if status.State != runtime.HealthError {
		t.Fatalf("state = %q", status.State)
	}
}

func TestHealth_ok(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "health.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	rt := NewRuntime(st)
	status := rt.Health(ctx)
	if status.State != runtime.HealthOK {
		t.Fatalf("state = %q details=%q", status.State, status.Details)
	}
}
