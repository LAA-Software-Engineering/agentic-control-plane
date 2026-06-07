package trace

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
)

func TestReader_ListByRunID_normalizesLegacyTypes(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "reader.db"))
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
	if _, err := st.AppendTraceEvent(ctx, "r1", start, "tool.called", "", "s1", `{}`); err != nil {
		t.Fatal(err)
	}

	events, err := NewReader(st).ListByRunID(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != string(EventToolSelection) {
		t.Fatalf("events=%+v", events)
	}
}

func TestNormalizeEvents_unknownTypePreserved(t *testing.T) {
	got := NormalizeEvents([]state.TraceEvent{{Type: "future_event", ActorType: "agent"}})
	if got[0].Type != "future_event" {
		t.Fatalf("type=%q", got[0].Type)
	}
}
