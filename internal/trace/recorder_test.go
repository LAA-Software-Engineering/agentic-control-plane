package trace

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
)

func TestRecorder_Append_increasingSeqPerRunID(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "trace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	started := time.Date(2026, 4, 11, 9, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID:        "run-a",
		WorkflowName: "wf",
		Env:          "dev",
		Status:       "running",
		StartedAt:    started,
		InputJSON:    `{}`,
		TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}

	fixed := started.Add(time.Minute)
	rec := NewRecorder(st)
	rec.Clock = func() time.Time { return fixed }

	seq1, err := rec.Append(ctx, "run-a", "s1", EventStepStarted, map[string]any{"x": 1})
	if err != nil {
		t.Fatal(err)
	}
	seq2, err := rec.Append(ctx, "run-a", "s1", EventStepFinished, map[string]any{"ok": true})
	if err != nil {
		t.Fatal(err)
	}
	if seq1 != 1 || seq2 != 2 {
		t.Fatalf("seq = %d, %d want 1, 2", seq1, seq2)
	}

	rd := NewReader(st)
	events, err := rd.ListByRunID(ctx, "run-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].DataJSON != `{"x":1}` || events[1].DataJSON != `{"ok":true}` {
		t.Fatalf("data json = %q, %q", events[0].DataJSON, events[1].DataJSON)
	}
}

func TestRecorder_Append_missingRunFailsWithErrRunNotFound(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "trace2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	rec := NewRecorder(st)
	_, err = rec.Append(ctx, "missing-run", "", EventRunStarted, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("want ErrRunNotFound in chain, got %v", err)
	}
	if !strings.Contains(err.Error(), "missing-run") {
		t.Fatalf("expected clear error mentioning run id, got: %v", err)
	}
}
