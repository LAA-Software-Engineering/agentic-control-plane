package inspect

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/statejson"
)

// TestContract_runEvents_matchesLogsJSON ensures inspector /api/runs/{id} events match agentctl logs -o json.
func TestContract_runEvents_matchesLogsJSON(t *testing.T) {
	ts := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	events := []state.TraceEvent{
		{RunID: "r1", Seq: 1, Timestamp: ts, Type: "run.started", DataJSON: `{}`},
		{RunID: "r1", Seq: 2, Timestamp: ts.Add(time.Second), Type: "tool.called", StepID: "s1", DataJSON: `{"x":1}`},
	}

	logsEvents := statejson.RunEventsPayload{RunID: "r1", Events: statejson.TraceEvents(events)}.Events
	inspectEvents := statejson.TraceEvents(events)

	logsJSON, err := json.Marshal(logsEvents)
	if err != nil {
		t.Fatal(err)
	}
	inspectJSON, err := json.Marshal(inspectEvents)
	if err != nil {
		t.Fatal(err)
	}
	if string(logsJSON) != string(inspectJSON) {
		t.Fatalf("events JSON mismatch:\n%s\n%s", logsJSON, inspectJSON)
	}
}
