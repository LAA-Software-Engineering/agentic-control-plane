package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

func TestOpen_createsTablesAndRoundTripAppliedResource(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "state.db")

	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	row := state.AppliedResource{
		Kind:               spec.KindAgent,
		Name:               "reviewer",
		Env:                "dev",
		SpecHash:           "abc123",
		NormalizedSpecJSON: `{"model":"m"}`,
		AppliedAt:          now,
	}
	if err := st.UpsertAppliedResource(ctx, row); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetAppliedResource(ctx, "dev", spec.ResourceID{Kind: spec.KindAgent, Name: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	if got.SpecHash != row.SpecHash || got.NormalizedSpecJSON != row.NormalizedSpecJSON {
		t.Fatalf("Get mismatch: %+v", got)
	}
	if !got.AppliedAt.Equal(now) {
		t.Fatalf("AppliedAt = %v want %v", got.AppliedAt, now)
	}

	list, err := st.ListAppliedResourcesByEnv(ctx, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "reviewer" {
		t.Fatalf("List = %+v", list)
	}

	row.SpecHash = "updated"
	row.AppliedAt = now.Add(time.Hour)
	if err := st.UpsertAppliedResource(ctx, row); err != nil {
		t.Fatal(err)
	}
	got2, err := st.GetAppliedResource(ctx, "dev", spec.ResourceID{Kind: spec.KindAgent, Name: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	if got2.SpecHash != "updated" {
		t.Fatalf("after upsert SpecHash = %q", got2.SpecHash)
	}
}

func TestMigrate_idempotent(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "m.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestOpen_twiceSameFile(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "reuse.db")
	s1, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	_ = s1.Close()

	s2, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
}

func TestRuntime_insertRunEventsQueryByRunID(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	started := time.Date(2026, 4, 11, 10, 0, 0, 0, time.UTC)
	run := state.Run{
		RunID:        "run-1",
		WorkflowName: "wf-a",
		Env:          "dev",
		Status:       "running",
		StartedAt:    started,
		InputJSON:    `{"k":1}`,
		TotalCostUSD: 0,
	}
	if err := st.StartRun(ctx, run); err != nil {
		t.Fatal(err)
	}

	stepStarted := started.Add(time.Minute)
	if err := st.UpsertRunStep(ctx, state.RunStep{
		RunID:     run.RunID,
		StepID:    "s1",
		Status:    "ok",
		StartedAt: &stepStarted,
		InputJSON: `{}`,
		CostUSD:   0.01,
	}); err != nil {
		t.Fatal(err)
	}

	ts1 := started.Add(2 * time.Minute)
	seq1, err := st.AppendTraceEvent(ctx, run.RunID, ts1, "log", "system", "", `{"m":"a"}`)
	if err != nil {
		t.Fatal(err)
	}
	ts2 := started.Add(3 * time.Minute)
	seq2, err := st.AppendTraceEvent(ctx, run.RunID, ts2, "metric", "system", "s1", `{"cpu":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if seq1 != 1 || seq2 != 2 {
		t.Fatalf("seq = %d, %d want 1, 2", seq1, seq2)
	}

	events, err := st.ListTraceEventsByRunID(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d", len(events))
	}
	if events[0].Seq != 1 || events[0].Type != "log" || events[0].DataJSON != `{"m":"a"}` {
		t.Fatalf("event[0] = %+v", events[0])
	}
	if events[1].Seq != 2 || events[1].StepID != "s1" {
		t.Fatalf("event[1] = %+v", events[1])
	}

	steps, err := st.ListRunStepsByRunID(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].StepID != "s1" || steps[0].CostUSD != 0.01 {
		t.Fatalf("steps = %+v", steps)
	}

	fin := started.Add(4 * time.Minute)
	if err := st.FinishRun(ctx, run.RunID, "succeeded", fin, `{"out":true}`, "", 0.02); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetRun(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "succeeded" || got.OutputJSON != `{"out":true}` || got.TotalCostUSD != 0.02 {
		t.Fatalf("GetRun = %+v", got)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(fin) {
		t.Fatalf("FinishedAt = %v want %v", got.FinishedAt, fin)
	}
}

func TestAppendTraceEvent_foreignKeyRequiresRun(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "fk.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	_, err = st.AppendTraceEvent(ctx, "no-such-run", time.Now().UTC(), "log", "system", "", `{}`)
	if err == nil {
		t.Fatal("expected error for missing run_id")
	}
}

func TestGetAppliedResource_notFound(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "nf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	_, err = st.GetAppliedResource(ctx, "dev", spec.ResourceID{Kind: spec.KindTool, Name: "nope"})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("want ErrNoRows, got %v", err)
	}
}

func TestListRecentRuns_and_ListRunsByWorkflow_order(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "listruns.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	t0 := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	if err := st.StartRun(ctx, state.Run{
		RunID: "older", WorkflowName: "wf-a", Env: "local", Status: "running",
		StartedAt: t0, InputJSON: `{}`, TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.StartRun(ctx, state.Run{
		RunID: "newer", WorkflowName: "wf-b", Env: "local", Status: "running",
		StartedAt: t1, InputJSON: `{}`, TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}

	recent, err := st.ListRecentRuns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].RunID != "newer" || recent[1].RunID != "older" {
		t.Fatalf("ListRecentRuns = %#v", recent)
	}

	byA, err := st.ListRunsByWorkflow(ctx, "wf-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(byA) != 1 || byA[0].RunID != "older" {
		t.Fatalf("ListRunsByWorkflow wf-a = %#v", byA)
	}
}

func TestDeleteRunsStartedBefore_cascadesChildRows(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "prune.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	oldStart := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	newStart := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: "old-run", WorkflowName: "wf", Env: "local", Status: "succeeded",
		StartedAt: oldStart, InputJSON: `{}`, TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertRunStep(ctx, state.RunStep{
		RunID: "old-run", StepID: "s1", Status: "done", StartedAt: &oldStart,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTraceEvent(ctx, "old-run", oldStart, "log", "system", "", `{}`); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveCheckpoint(ctx, state.RunCheckpoint{
		RunID: "old-run", StepIndex: 0, StepID: "s1",
		ContextJSON: `{"version":1,"input":{},"steps":{},"totalCostUsd":0}`,
		Status:      state.CheckpointStatusRunning, CreatedAt: oldStart,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.StartRun(ctx, state.Run{
		RunID: "new-run", WorkflowName: "wf", Env: "local", Status: "running",
		StartedAt: newStart, InputJSON: `{}`, TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}

	cutoff := time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC)
	n, err := st.DeleteRunsStartedBefore(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("RowsAffected = %d want 1", n)
	}

	if _, err := st.GetRun(ctx, "old-run"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("old run: %v", err)
	}
	evs, err := st.ListTraceEventsByRunID(ctx, "old-run")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 0 {
		t.Fatalf("trace events for deleted run: %d", len(evs))
	}
	if _, err := st.GetLatestCheckpoint(ctx, "old-run"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("checkpoint for deleted run: %v", err)
	}
	got, err := st.GetRun(ctx, "new-run")
	if err != nil {
		t.Fatal(err)
	}
	if got.RunID != "new-run" {
		t.Fatalf("GetRun new: %+v", got)
	}
}

func TestSaveCheckpoint_roundTripAndLatest(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "cp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	start := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: "r1", WorkflowName: "wf", Env: "local", Status: "running",
		StartedAt: start, InputJSON: `{"x":1}`, TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}

	cp1 := state.RunCheckpoint{
		RunID: "r1", StepIndex: 0, StepID: "step-a",
		ContextJSON: `{"version":1,"input":{"x":1},"steps":{"step-a":{"output":{"ok":true}}},"totalCostUsd":0.01}`,
		Status:      state.CheckpointStatusRunning, CreatedAt: start,
	}
	if err := st.SaveCheckpoint(ctx, cp1); err != nil {
		t.Fatal(err)
	}
	later := start.Add(time.Minute)
	cp2 := state.RunCheckpoint{
		RunID: "r1", StepIndex: 1, StepID: "step-b",
		ContextJSON: `{"version":1,"input":{"x":1},"steps":{},"totalCostUsd":0.02}`,
		Status:      state.CheckpointStatusInterrupted, CreatedAt: later,
	}
	if err := st.SaveCheckpoint(ctx, cp2); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetLatestCheckpoint(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Seq != 2 {
		t.Fatalf("Seq = %d want 2", got.Seq)
	}
	if got.StepIndex != 1 || got.StepID != "step-b" {
		t.Fatalf("step = index %d id %q", got.StepIndex, got.StepID)
	}
	if got.Status != state.CheckpointStatusInterrupted {
		t.Fatalf("Status = %q", got.Status)
	}
	if got.ContextJSON != cp2.ContextJSON {
		t.Fatalf("ContextJSON = %q", got.ContextJSON)
	}
	if !got.CreatedAt.Equal(later) {
		t.Fatalf("CreatedAt = %v", got.CreatedAt)
	}

	all, err := st.ListCheckpointsByRunID(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Seq != 1 || all[1].Seq != 2 {
		t.Fatalf("ListCheckpointsByRunID = %+v", all)
	}
}

func TestSaveCheckpoint_foreignKeyRequiresRun(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "cp-fk.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	err = st.SaveCheckpoint(ctx, state.RunCheckpoint{
		RunID: "missing", StepIndex: 0, StepID: "s",
		ContextJSON: `{}`, Status: state.CheckpointStatusRunning, CreatedAt: now,
	})
	if err == nil {
		t.Fatal("expected FK error")
	}
}

func TestGetLatestCheckpoint_noRows(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "cp-none.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := st.GetLatestCheckpoint(ctx, "nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateRunStatus(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "status.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	start := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: "r1", WorkflowName: "wf", Env: "local", Status: "running",
		StartedAt: start, InputJSON: `{}`, TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateRunStatus(ctx, "r1", "interrupted"); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetRun(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "interrupted" {
		t.Fatalf("status = %q", got.Status)
	}
	if got.FinishedAt != nil {
		t.Fatalf("FinishedAt = %v want nil", got.FinishedAt)
	}
	if err := st.UpdateRunStatus(ctx, "missing", "running"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing run: %v", err)
	}
}
