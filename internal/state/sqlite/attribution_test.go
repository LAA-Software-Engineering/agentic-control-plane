package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

func TestStartRun_attributionDefaultsAndTracePropagation(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "attr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	start := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: "run-attr", WorkflowName: "wf", Env: "local", Status: state.RunStatusRunning,
		StartedAt: start, InputJSON: `{}`, TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetRun(ctx, "run-attr")
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != state.DefaultTenantID || got.ThreadID != state.DefaultThreadID || got.ActorID != state.DefaultActorID {
		t.Fatalf("defaults: %+v", got)
	}
	if got.Source != state.DefaultSource {
		t.Fatalf("source = %q", got.Source)
	}
	if got.RequestID != "run-attr" {
		t.Fatalf("request_id = %q want run-attr", got.RequestID)
	}

	if _, err := st.AppendTraceEvent(ctx, "run-attr", start, "run.started", "", `{}`); err != nil {
		t.Fatal(err)
	}
	events, err := st.ListTraceEventsByRunID(ctx, "run-attr")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	if events[0].TenantID != state.DefaultTenantID || events[0].ThreadID != state.DefaultThreadID || events[0].ActorID != state.DefaultActorID {
		t.Fatalf("trace attribution: %+v", events[0])
	}
}

func TestStartRun_explicitAttribution(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "attr-explicit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	start := time.Now().UTC()
	state.ApplyAttribution(&state.Run{
		RunID: "r2", WorkflowName: "wf", Env: "staging", Status: state.RunStatusRunning,
		StartedAt: start, InputJSON: `{}`,
	}, state.RunAttribution{
		TenantID: "acme", ThreadID: "prod-thread", ActorID: "ci-bot",
		ParentRunID: "parent-1", RequestID: "req-99", IdempotencyKey: "idem-1", Source: "actions",
	})
	run := state.Run{
		RunID: "r2", WorkflowName: "wf", Env: "staging", Status: state.RunStatusRunning,
		StartedAt: start, InputJSON: `{}`,
		TenantID: "acme", ThreadID: "prod-thread", ActorID: "ci-bot",
		ParentRunID: "parent-1", RequestID: "req-99", IdempotencyKey: "idem-1", Source: "actions",
	}
	if err := st.StartRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetRun(ctx, "r2")
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "acme" || got.ThreadID != "prod-thread" || got.ActorID != "ci-bot" {
		t.Fatalf("attribution: %+v", got)
	}
	if got.ParentRunID != "parent-1" || got.RequestID != "req-99" || got.IdempotencyKey != "idem-1" || got.Source != "actions" {
		t.Fatalf("metadata: %+v", got)
	}
}

func TestListRunsFiltered(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "filter.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	t0 := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	runs := []state.Run{
		{RunID: "a", WorkflowName: "wf", Env: "local", Status: "running", StartedAt: t0,
			InputJSON: `{}`, TenantID: "t1", ThreadID: "th1", ActorID: "u1", RequestID: "ra", Source: "cli"},
		{RunID: "b", WorkflowName: "wf", Env: "local", Status: "running", StartedAt: t0.Add(time.Hour),
			InputJSON: `{}`, TenantID: "t1", ThreadID: "th2", ActorID: "u1", RequestID: "rb", Source: "cli"},
		{RunID: "c", WorkflowName: "other", Env: "local", Status: "running", StartedAt: t0.Add(2 * time.Hour),
			InputJSON: `{}`, TenantID: "t2", ThreadID: "th1", ActorID: "u2", RequestID: "rc", Source: "cli"},
	}
	for _, r := range runs {
		if err := st.StartRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	filtered, err := st.ListRunsFiltered(ctx, state.RunListFilter{TenantID: "t1", ThreadID: "th1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].RunID != "a" {
		t.Fatalf("tenant+thread: %#v", filtered)
	}

	byActor, err := st.ListRunsFiltered(ctx, state.RunListFilter{ActorID: "u2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byActor) != 1 || byActor[0].RunID != "c" {
		t.Fatalf("actor: %#v", byActor)
	}

	byWF, err := st.ListRunsFiltered(ctx, state.RunListFilter{WorkflowName: "wf", TenantID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byWF) != 2 {
		t.Fatalf("workflow+tenant: %#v", byWF)
	}
}

func TestMigrate_attributionBackfillFromPre004DB(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	// Apply migrations through 004 only (pre-attribution schema).
	for _, ver := range []int{1, 2, 3, 4} {
		if err := applySingleMigration(ctx, db, ver); err != nil {
			t.Fatalf("migration %d: %v", ver, err)
		}
	}

	start := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `
INSERT INTO runs (run_id, workflow_name, env, status, started_at, input_json, total_cost_usd, workflow_spec_hash, environment_name)
VALUES ('legacy-run', 'wf', 'local', 'running', ?, '{}', 0, '', '')
`, start.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO trace_events (run_id, seq, timestamp, type, data_json)
VALUES ('legacy-run', 1, ?, 'log', '{}')
`, start.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	if err := applySingleMigration(ctx, db, 5); err != nil {
		t.Fatal(err)
	}

	var tenant, thread, actor, requestID string
	if err := db.QueryRowContext(ctx, `
SELECT tenant_id, thread_id, actor_id, request_id FROM runs WHERE run_id = 'legacy-run'
`).Scan(&tenant, &thread, &actor, &requestID); err != nil {
		t.Fatal(err)
	}
	if tenant != state.DefaultTenantID || thread != state.DefaultThreadID || actor != state.DefaultActorID {
		t.Fatalf("run backfill: %s %s %s", tenant, thread, actor)
	}
	if requestID != "legacy-run" {
		t.Fatalf("request_id = %q", requestID)
	}
	if err := db.QueryRowContext(ctx, `
SELECT tenant_id, thread_id, actor_id FROM trace_events WHERE run_id = 'legacy-run'
`).Scan(&tenant, &thread, &actor); err != nil {
		t.Fatal(err)
	}
	if tenant != state.DefaultTenantID || thread != state.DefaultThreadID || actor != state.DefaultActorID {
		t.Fatalf("trace backfill: %s %s %s", tenant, thread, actor)
	}
}
