package sqlite

import (
	"context"
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
	if _, err := st.AppendTraceEvent(ctx, "legacy", start, "run.started", string(trace.ActorAgent), "", `{}`); err != nil {
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
	if events[0].ActorType != string(trace.ActorAgent) {
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
	if _, err := st.AppendTraceEvent(ctx, "r1", start, string(trace.EventHitlRequestCreated), string(trace.ActorSystem), "", `{}`); err != nil {
		t.Fatal(err)
	}
	events, err := st.ListTraceEventsByRunID(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ActorType != string(trace.ActorSystem) {
		t.Fatalf("events=%+v", events)
	}
}
