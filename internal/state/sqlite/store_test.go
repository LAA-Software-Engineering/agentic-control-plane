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
