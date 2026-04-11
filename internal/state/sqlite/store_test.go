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
	seq1, err := st.AppendTraceEvent(ctx, run.RunID, ts1, "log", "", `{"m":"a"}`)
	if err != nil {
		t.Fatal(err)
	}
	ts2 := started.Add(3 * time.Minute)
	seq2, err := st.AppendTraceEvent(ctx, run.RunID, ts2, "metric", "s1", `{"cpu":1}`)
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

	_, err = st.AppendTraceEvent(ctx, "no-such-run", time.Now().UTC(), "log", "", `{}`)
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
