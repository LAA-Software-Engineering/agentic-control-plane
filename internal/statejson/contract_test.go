package statejson

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

func TestContract_traceEvents_matchLogsShape(t *testing.T) {
	ts := time.Date(2026, 6, 4, 10, 0, 0, 0, time.UTC)
	events := []state.TraceEvent{
		{RunID: "r1", Seq: 1, Timestamp: ts, Type: "run.started", DataJSON: `{"k":1}`},
		{RunID: "r1", Seq: 2, Timestamp: ts.Add(time.Second), Type: "run.finished", StepID: "s1", DataJSON: ""},
	}
	got, err := json.Marshal(TraceEvents(events))
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"seq":1,"timestamp":"2026-06-04T10:00:00Z","type":"run.started","data":{"k":1}},` +
		`{"seq":2,"timestamp":"2026-06-04T10:00:01Z","type":"run.finished","stepId":"s1","data":{}}]`
	if string(got) != want {
		t.Fatalf("marshal:\n%s\nwant:\n%s", got, want)
	}
}

func TestContract_runRecord_matchesLogsFields(t *testing.T) {
	start := time.Date(2026, 6, 4, 9, 0, 0, 0, time.UTC)
	fin := start.Add(time.Minute)
	r := state.Run{
		RunID: "id-1", WorkflowName: "wf", Env: "staging", Status: "succeeded",
		StartedAt: start, FinishedAt: &fin, InputJSON: `{"a":1}`, OutputJSON: `{"b":2}`,
		ErrorText: "", TotalCostUSD: 0.5,
	}
	rec := Run(r)
	if rec.RunID != "id-1" || rec.Workflow != "wf" || rec.Env != "staging" {
		t.Fatalf("record=%+v", rec)
	}
	if rec.StartedAt != start.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("startedAt=%q", rec.StartedAt)
	}
	if rec.FinishedAt != fin.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("finishedAt=%q", rec.FinishedAt)
	}
	if string(rec.Input) != `{"a":1}` || string(rec.Output) != `{"b":2}` {
		t.Fatalf("input/output=%s %s", rec.Input, rec.Output)
	}
}

func TestContract_run_emptyInputDefaultsToObject(t *testing.T) {
	r := state.Run{
		RunID: "r", WorkflowName: "w", Env: "local", Status: "running",
		StartedAt: time.Now().UTC(), InputJSON: "",
	}
	rec := Run(r)
	if string(rec.Input) != `{}` {
		t.Fatalf("input=%s want {}", rec.Input)
	}
}

func TestContract_appliedResource_matchesStateCLI(t *testing.T) {
	at := time.Date(2026, 6, 4, 8, 0, 0, 0, time.UTC)
	row := state.AppliedResource{
		Kind: "Agent", Name: "a", Env: "local", SpecHash: "h",
		NormalizedSpecJSON: `{"m":1}`, AppliedAt: at,
	}
	got, err := json.Marshal(AppliedResource(row))
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"kind":"Agent","name":"a","env":"local","specHash":"h","appliedAt":"2026-06-04T08:00:00Z","normalizedSpecJson":"{\"m\":1}"}`
	if string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
