package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

func TestOpenReadOnly_rejectsWrites(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "ro.db")

	rw, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	run := state.Run{
		RunID:        "r1",
		WorkflowName: "wf",
		Env:          "local",
		Status:       state.RunStatusRunning,
		StartedAt:    time.Now().UTC(),
		InputJSON:    `{}`,
	}
	if err := rw.StartRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := rw.Close(); err != nil {
		t.Fatal(err)
	}

	ro, err := OpenReadOnly(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ro.Close() })

	got, err := ro.GetRun(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if got.RunID != "r1" {
		t.Fatalf("GetRun = %+v", got)
	}

	err = ro.StartRun(ctx, state.Run{
		RunID:        "r2",
		WorkflowName: "wf",
		Env:          "local",
		Status:       state.RunStatusRunning,
		StartedAt:    time.Now().UTC(),
		InputJSON:    `{}`,
	})
	if err == nil {
		t.Fatal("expected write on read-only store to fail")
	}
}

func TestOpenReadOnly_missingFile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "missing.db")
	_, err := OpenReadOnly(ctx, path)
	if err == nil {
		t.Fatal("expected error for missing database")
	}
}
