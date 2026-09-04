package local

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/config"
	"github.com/Terfyn/terfyn/internal/project"
	"github.com/Terfyn/terfyn/internal/runtime"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
	"github.com/Terfyn/terfyn/internal/trace"
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
	if events[0].Type != string(trace.EventRunStarted) {
		t.Fatalf("first event %q", events[0].Type)
	}
	if events[len(events)-1].Type != string(trace.EventRunFinished) {
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
	if _, err := st.AppendTraceEvent(ctx, oldID, oldStart, string(trace.EventRunStarted), "agent", "", `{}`); err != nil {
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

// TestResume_preservesAttribution proves resume reuses the run's ORIGINAL
// attribution (run row + resume trace), ignoring resume-time overrides. The run
// interrupts at a real HITL gate (the `gated` fixture workflow) — no test hook.
func TestResume_preservesAttribution(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "resume-attr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := testRunProjRoot(t)
	rc := testResolvedConfig(t, root, "staging")

	runID := "resume-attr-1"
	started := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	inputJSON := []byte(`{"topic":"resume-attr"}`)

	// Start through the runtime (not a hand-seeded run + bare executor) so the pinned .agent program is
	// the SAME on the interrupting run and the resume — a mixed DAG-start / program-resume would not
	// restore the checkpoint. The gate interrupts cleanly (nil err, status interrupted).
	rtStart := NewRuntime(st)
	rtStart.Now = func() time.Time { return started }
	if _, err := rtStart.Invoke(ctx, rc, runtime.InvokeOptions{
		RunID: runID, WorkflowName: "gated", Env: "dev", EnvironmentName: "staging",
		InputJSON: inputJSON,
		TenantID:  "acme", ThreadID: "thread-original", ActorID: "starter-bot",
		RequestID: "req-original", Source: "cli",
	}); err != nil {
		t.Fatalf("gated invoke should interrupt cleanly at the gate, got %v", err)
	}
	if r, err := st.GetRun(ctx, runID); err != nil || r.Status != state.RunStatusInterrupted {
		t.Fatalf("gated run should be interrupted at the gate, got status=%q err=%v", r.Status, err)
	}

	rt := NewRuntime(st)
	rt.Now = func() time.Time { return started.Add(time.Hour) }
	if _, err := rt.Resume(ctx, rc, runtime.ResumeOptions{
		RunID: runID, EnvironmentName: "staging",
		HitlActor:    "approver",
		HitlDecision: &runtime.HitlDecisionOptions{Kind: spec.HitlDecisionApprove},
		TenantID:     "other-tenant", ThreadID: "thread-override", ActorID: "other-actor",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.RunStatusSucceeded {
		t.Fatalf("resume status = %q err=%q", got.Status, got.ErrorText)
	}
	if got.TenantID != "acme" || got.ThreadID != "thread-original" || got.ActorID != "starter-bot" {
		t.Fatalf("run attribution changed: %+v", got)
	}

	events, err := trace.NewReader(st).ListByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Type == string(trace.EventRunStarted) && strings.Contains(ev.DataJSON, `"resumed":true`) {
			if ev.TenantID != "acme" || ev.ThreadID != "thread-original" || ev.ActorID != "starter-bot" {
				t.Fatalf("resume trace attribution: %+v", ev)
			}
		}
	}
}
