package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/audit"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

func startTestRun(t *testing.T, ctx context.Context, st *Store, runID string, start time.Time) {
	t.Helper()
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "wf", Env: "dev", Status: "running",
		StartedAt: start, InputJSON: `{}`,
		TenantID: "tenant-1", ThreadID: "thread-1", ActorID: "actor-1",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAppendTraceEvent_buildsHashChain(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	start := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	startTestRun(t, ctx, st, "run-1", start)

	if _, err := st.AppendTraceEvent(ctx, "run-1", start, "run_started", "agent", "", `{}`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTraceEvent(ctx, "run-1", start.Add(time.Second), "tool_execution", "agent", "s1", `{"x":1}`); err != nil {
		t.Fatal(err)
	}

	events, err := st.ListTraceEventsByRunID(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events=%d", len(events))
	}
	if events[0].PrevHash != audit.GenesisHash("run-1") || events[0].Hash == "" {
		t.Fatalf("event[0] chain=%+v", events[0])
	}
	if events[1].PrevHash != events[0].Hash || events[1].Hash == "" {
		t.Fatalf("event[1] chain=%+v", events[1])
	}
	if err := audit.VerifyRunChainError("run-1", events); err != nil {
		t.Fatal(err)
	}
}

func TestAppendTraceEvent_concurrentAppendsNoFork(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "conc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	start := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	startTestRun(t, ctx, st, "run-conc", start)

	const n = 8
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			_, err := st.AppendTraceEvent(ctx, "run-conc", start.Add(time.Duration(i)*time.Millisecond), "tool_execution", "agent", "", `{}`)
			errCh <- err
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}

	events, err := st.ListTraceEventsByRunID(ctx, "run-conc")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != n {
		t.Fatalf("events=%d want %d", len(events), n)
	}
	seen := make(map[int64]struct{}, n)
	for _, e := range events {
		if e.Hash == "" {
			t.Fatalf("seq %d missing hash", e.Seq)
		}
		if _, ok := seen[e.Seq]; ok {
			t.Fatalf("duplicate seq %d", e.Seq)
		}
		seen[e.Seq] = struct{}{}
	}
	if err := audit.VerifyRunChainError("run-conc", events); err != nil {
		t.Fatal(err)
	}
}

func TestMigrate_auditChainColumnsFromPre007DB(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "pre007.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	for ver := 1; ver <= 6; ver++ {
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
	if _, err := db.ExecContext(ctx, `
INSERT INTO trace_events (run_id, seq, timestamp, type, data_json, tenant_id, thread_id, actor_id, actor_type)
VALUES ('legacy-run', 1, ?, 'run_started', '{}', 'tenant-1', 'thread-1', 'user-1', 'agent')
`, ts); err != nil {
		t.Fatal(err)
	}

	if err := applySingleMigration(ctx, db, 7); err != nil {
		t.Fatal(err)
	}

	var prevHash, eventHash sql.NullString
	if err := db.QueryRowContext(ctx, `
SELECT prev_hash, hash FROM trace_events WHERE run_id = 'legacy-run' AND seq = 1
`).Scan(&prevHash, &eventHash); err != nil {
		t.Fatal(err)
	}
	if prevHash.Valid || eventHash.Valid {
		t.Fatalf("legacy row should remain unchained: prev=%v hash=%v", prevHash, eventHash)
	}

	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := st.AppendTraceEvent(ctx, "legacy-run", start.Add(time.Second), "run_finished", "agent", "", `{}`); err != nil {
		t.Fatal(err)
	}
	events, err := st.ListTraceEventsByRunID(ctx, "legacy-run")
	if err != nil {
		t.Fatal(err)
	}
	res := audit.VerifyRunChain("legacy-run", events)
	if !res.Ok() || res.Unchained != 1 || res.Chained != 1 {
		t.Fatalf("res=%+v events=%+v", res, events)
	}
}

func TestAppendTraceEvent_chainsAfterMiddleUnchainedGap(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "gap.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	start := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	startTestRun(t, ctx, st, "run-gap", start)
	if _, err := st.AppendTraceEvent(ctx, "run-gap", start, "run_started", "agent", "", `{}`); err != nil {
		t.Fatal(err)
	}
	events, err := st.ListTraceEventsByRunID(ctx, "run-gap")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Hash == "" {
		t.Fatalf("event[0]=%+v", events[0])
	}
	tip := events[0].Hash
	ts := start.UTC().Format(time.RFC3339Nano)
	if _, err := st.db.ExecContext(ctx, `
INSERT INTO trace_events (run_id, seq, timestamp, type, data_json, tenant_id, thread_id, actor_id, actor_type)
VALUES ('run-gap', 2, ?, 'tool_execution', '{}', 'tenant-1', 'thread-1', 'actor-1', 'agent')
`, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTraceEvent(ctx, "run-gap", start.Add(2*time.Second), "run_finished", "agent", "", `{}`); err != nil {
		t.Fatal(err)
	}
	events, err = st.ListTraceEventsByRunID(ctx, "run-gap")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("events=%d", len(events))
	}
	if events[2].PrevHash != tip {
		t.Fatalf("seq3 prev=%q want tip %q", events[2].PrevHash, tip)
	}
	if err := audit.VerifyRunChainError("run-gap", events); err != nil {
		t.Fatal(err)
	}
}

func TestAuditVerify_detectsTamperedRow(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "tamper.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	start := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	startTestRun(t, ctx, st, "run-t", start)
	if _, err := st.AppendTraceEvent(ctx, "run-t", start, "run_started", "agent", "", `{}`); err != nil {
		t.Fatal(err)
	}

	if _, err := st.db.ExecContext(ctx, `UPDATE trace_events SET data_json = '{"tampered":true}' WHERE run_id = 'run-t' AND seq = 1`); err != nil {
		t.Fatal(err)
	}
	events, err := st.ListTraceEventsByRunID(ctx, "run-t")
	if err != nil {
		t.Fatal(err)
	}
	res := audit.VerifyRunChain("run-t", events)
	if res.Ok() || res.BrokenSeq != 1 || res.BrokenField != audit.BrokenFieldHash {
		t.Fatalf("res=%+v", res)
	}
}
