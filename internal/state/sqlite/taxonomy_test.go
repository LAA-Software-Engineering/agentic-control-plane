package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

func TestListTraceEventsByRunID_normalizesLegacyTypeOnRead(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "tax.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	start := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: "legacy", WorkflowName: "wf", Env: "dev", Status: "running",
		StartedAt: start, InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTraceEvent(ctx, "legacy", start, "run.started", state.TraceActorTypeAgent, "", `{}`); err != nil {
		t.Fatal(err)
	}

	events, err := trace.NewReader(st).ListByRunID(ctx, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%d", len(events))
	}
	if events[0].Type != string(trace.EventRunStarted) {
		t.Fatalf("type=%q", events[0].Type)
	}
	if events[0].ActorType != state.TraceActorTypeAgent {
		t.Fatalf("actorType=%q", events[0].ActorType)
	}
}

func TestAppendTraceEvent_persistsActorType(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "actor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	start := time.Now().UTC()
	if err := st.StartRun(ctx, state.Run{
		RunID: "r1", WorkflowName: "wf", Env: "dev", Status: "running",
		StartedAt: start, InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTraceEvent(ctx, "r1", start, string(trace.EventHitlRequestCreated), state.TraceActorTypeSystem, "", `{}`); err != nil {
		t.Fatal(err)
	}
	events, err := st.ListTraceEventsByRunID(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ActorType != state.TraceActorTypeSystem {
		t.Fatalf("events=%+v", events)
	}
}

func TestMigrate_traceTaxonomyBackfillFromPre006DB(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "pre006.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	for _, ver := range []int{1, 2, 3, 4, 5} {
		if err := applySingleMigration(ctx, db, ver); err != nil {
			t.Fatalf("migration %d: %v", ver, err)
		}
	}

	start := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	ts := start.UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `
INSERT INTO runs (run_id, workflow_name, env, status, started_at, input_json, total_cost_usd, workflow_spec_hash, environment_name,
  tenant_id, thread_id, actor_id, request_id, source)
VALUES ('legacy-run', 'wf', 'local', 'running', ?, '{}', 0, '', '', 'tenant-1', 'thread-1', 'user-1', 'legacy-run', 'cli')
`, ts); err != nil {
		t.Fatal(err)
	}

	legacyRows := []struct {
		seq       int
		typ       string
		wantType  string
		wantActor string
	}{
		{1, "run.started", string(trace.EventRunStarted), state.TraceActorTypeAgent},
		{2, "tool.called", string(trace.EventToolSelection), state.TraceActorTypeAgent},
		{3, "approval.requested", string(trace.EventHitlRequestCreated), state.TraceActorTypeSystem},
		{4, "policy.denied", string(trace.EventSystemError), state.TraceActorTypeSystem},
		{5, "custom", "custom", state.TraceActorTypeSystem},
	}
	for _, row := range legacyRows {
		if _, err := db.ExecContext(ctx, `
INSERT INTO trace_events (run_id, seq, timestamp, type, data_json, tenant_id, thread_id, actor_id)
VALUES ('legacy-run', ?, ?, ?, '{}', 'tenant-1', 'thread-1', 'user-1')
`, row.seq, ts, row.typ); err != nil {
			t.Fatalf("insert %q: %v", row.typ, err)
		}
	}

	if err := applySingleMigration(ctx, db, 6); err != nil {
		t.Fatal(err)
	}

	for _, row := range legacyRows {
		var gotType, gotActor string
		if err := db.QueryRowContext(ctx, `
SELECT type, actor_type FROM trace_events WHERE run_id = 'legacy-run' AND seq = ?
`, row.seq).Scan(&gotType, &gotActor); err != nil {
			t.Fatalf("seq %d: %v", row.seq, err)
		}
		if gotType != row.wantType || gotActor != row.wantActor {
			t.Fatalf("seq %d (%q): type=%q actor=%q want type=%q actor=%q",
				row.seq, row.typ, gotType, gotActor, row.wantType, row.wantActor)
		}
	}
}
