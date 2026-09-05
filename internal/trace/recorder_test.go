package trace

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
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

	seq1, err := rec.Append(ctx, "run-a", "s1", EventToolSelection, ActorAgent, map[string]any{"x": 1})
	if err != nil {
		t.Fatal(err)
	}
	seq2, err := rec.Append(ctx, "run-a", "s1", EventToolExecution, ActorAgent, map[string]any{"ok": true})
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
	if events[0].Type != string(EventToolSelection) || events[0].ActorType != string(ActorAgent) {
		t.Fatalf("first event = %+v", events[0])
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
	_, err = rec.Append(ctx, "missing-run", "", EventRunStarted, ActorAgent, nil)
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

// startRunForTest opens a fresh sqlite store with one running run and returns the store.
func startRunForTest(t *testing.T, runID string) *sqlite.Store {
	t.Helper()
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "sink.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "wf", Env: "dev", Status: "running",
		StartedAt: time.Now().UTC(), InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	return st
}

// TestRecorder_Sink_receivesRedactedData proves the live-stream hook is invoked once per successful
// append with the SAME redacted payload that is persisted — a secret-keyed value is masked in the
// stream, not just in storage (issue #450).
func TestRecorder_Sink_receivesRedactedData(t *testing.T) {
	ctx := context.Background()
	st := startRunForTest(t, "run-sink")

	var got []StreamEvent
	rec := NewRecorder(st)
	rec.Sink = func(ev StreamEvent) { got = append(got, ev) }

	seq, err := rec.Append(ctx, "run-sink", "s1", EventToolSelection, ActorAgent, map[string]any{"password": "s3cret", "x": 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("sink called %d times, want 1", len(got))
	}
	ev := got[0]
	if ev.Seq != seq || ev.RunID != "run-sink" || ev.StepID != "s1" || ev.Type != EventToolSelection {
		t.Fatalf("stream event = %+v (seq %d)", ev, seq)
	}
	if ev.Data["password"] != RedactedPlaceholder {
		t.Fatalf("sink must receive redacted data, got password=%v", ev.Data["password"])
	}
	if ev.Data["x"] == nil {
		t.Fatalf("non-secret field dropped from stream: %+v", ev.Data)
	}
}

// TestRecorder_Sink_notCalledOnPersistError proves the sink runs only after a successful persist:
// an append for a missing run errors and the sink is never invoked.
func TestRecorder_Sink_notCalledOnPersistError(t *testing.T) {
	ctx := context.Background()
	st := startRunForTest(t, "run-ok")

	called := 0
	rec := NewRecorder(st)
	rec.Sink = func(StreamEvent) { called++ }

	if _, err := rec.Append(ctx, "no-such-run", "", EventRunStarted, ActorAgent, nil); err == nil {
		t.Fatal("expected an error for a missing run")
	}
	if called != 0 {
		t.Fatalf("sink invoked %d times on a persist error, want 0", called)
	}
}

// TestRecorder_Sink_panicDoesNotFailAppend proves a panicking sink cannot turn a committed append
// into a returned error (the recorder comment's load-bearing guarantee): Append still returns the
// seq and a nil error, and the event is persisted.
func TestRecorder_Sink_panicDoesNotFailAppend(t *testing.T) {
	ctx := context.Background()
	st := startRunForTest(t, "run-panic")

	rec := NewRecorder(st)
	rec.Sink = func(StreamEvent) { panic("sink boom") }

	seq, err := rec.Append(ctx, "run-panic", "s1", EventToolSelection, ActorAgent, map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("a sink panic must not fail Append: %v", err)
	}
	if seq != 1 {
		t.Fatalf("seq = %d, want 1 (event still persisted)", seq)
	}
	events, err := NewReader(st).ListByRunID(ctx, "run-panic")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("event not persisted despite sink panic: %+v", events)
	}
}

func TestRecorder_Append_rejectsUnknownEventType(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "trace3.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	started := time.Now().UTC()
	if err := st.StartRun(ctx, state.Run{
		RunID: "r1", WorkflowName: "wf", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}

	rec := NewRecorder(st)
	_, err = rec.Append(ctx, "r1", "", EventType("free_form"), ActorAgent, nil)
	if err == nil {
		t.Fatal("expected validation error")
	}
}
