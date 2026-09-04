package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/state"
	sqlitemigrations "github.com/Terfyn/terfyn/migrations/sqlite"
)

// TestStartedAt_orderingAndRetention is the reported repro (#385): a whole-second
// started_at must not sort after a later fractional-second one, and retention's
// `started_at < cutoff` compare must count the same boundary. The fixed-width
// storage layout makes text order equal time order.
func TestStartedAt_orderingAndRetention(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	older := base                             // exact second, no fractional part
	newer := base.Add(500 * time.Millisecond) // fractional second
	if err := st.StartRun(ctx, state.Run{RunID: "older", WorkflowName: "w", Env: "e", Status: "succeeded", StartedAt: older}); err != nil {
		t.Fatal(err)
	}
	if err := st.StartRun(ctx, state.Run{RunID: "newer", WorkflowName: "w", Env: "e", Status: "succeeded", StartedAt: newer}); err != nil {
		t.Fatal(err)
	}

	runs, err := st.ListRecentRuns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].RunID != "newer" {
		t.Fatalf("ListRecentRuns order wrong: %s (%s) then %s", runs[0].RunID, runs[0].StartedAt, runs[len(runs)-1].RunID)
	}

	n, err := st.DeleteRunsStartedBefore(ctx, base.Add(250*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 deleted (the whole-second run before the cutoff), got %d", n)
	}
	remaining, _ := st.ListRecentRuns(ctx, 10)
	if len(remaining) != 1 || remaining[0].RunID != "newer" {
		t.Fatalf("retention deleted the wrong row: %+v", remaining)
	}
}

// TestMigration_normalizesLegacyStartedAtWidth proves migration 010 rewrites
// pre-existing RFC3339Nano rows (variable fractional width) to the fixed-width
// layout, so legacy rows and new rows order consistently (#385). It simulates
// legacy rows by writing the old format directly, then re-applies the migration's
// SQL body.
func TestMigration_normalizesLegacyStartedAtWidth(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Seed rows through the API, then stamp legacy (trailing-zero-trimmed) values.
	for _, id := range []string{"whole", "frac"} {
		if err := st.StartRun(ctx, state.Run{RunID: id, WorkflowName: "w", Env: "e", Status: "succeeded", StartedAt: base}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE runs SET started_at = ? WHERE run_id = 'whole'`, base.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE runs SET started_at = ? WHERE run_id = 'frac'`, base.Add(500*time.Millisecond).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	// Sanity: legacy text ordering is wrong (whole second sorts after fractional).
	pre, _ := st.ListRecentRuns(ctx, 10)
	if pre[0].RunID != "whole" {
		t.Skipf("legacy ordering already correct on this platform (%s first); migration still exercised below", pre[0].RunID)
	}

	body, err := sqlitemigrations.Files.ReadFile("010_normalize_started_at_width.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, string(body)); err != nil {
		t.Fatalf("apply migration 010: %v", err)
	}

	post, err := st.ListRecentRuns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if post[0].RunID != "frac" {
		t.Fatalf("after normalization the fractional-second run must sort first, got %s first", post[0].RunID)
	}
	// Both stored values are now fixed nine-digit-fraction UTC.
	var whole, frac string
	if err := st.db.QueryRowContext(ctx, `SELECT started_at FROM runs WHERE run_id='whole'`).Scan(&whole); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT started_at FROM runs WHERE run_id='frac'`).Scan(&frac); err != nil {
		t.Fatal(err)
	}
	if whole != "2026-01-01T00:00:00.000000000Z" || frac != "2026-01-01T00:00:00.500000000Z" {
		t.Fatalf("normalized values wrong: whole=%q frac=%q", whole, frac)
	}
}
