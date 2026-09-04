package engine

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
	"github.com/Terfyn/terfyn/internal/trace"
)

// TestRedactStepJSON_masksSensitiveArgs is the write-layer half of the #408 fix: run_steps input/output
// are persisted through the same redaction the trace recorder applies, so a token/password/authorization
// argument is masked in the run_steps table instead of stored in clear and served verbatim by inspect.
// A map payload is redacted key-wise; a non-sensitive value survives; a scalar (no keys) passes through.
func TestRedactStepJSON_masksSensitiveArgs(t *testing.T) {
	a := &engineInvoker{e: &Executor{Trace: trace.NewRecorderForGraph(nil, nil)}}

	b := a.redactStepJSON(map[string]any{"token": "sekret-123", "topic": "hi"})
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("redacted step JSON is not valid JSON: %v\n%s", err, b)
	}
	if m["token"] != trace.RedactedPlaceholder {
		t.Fatalf("token must be redacted in run_steps input, got %v", m["token"])
	}
	if m["topic"] != "hi" {
		t.Fatalf("non-sensitive arg must survive, got %v", m["topic"])
	}

	// A nested sensitive key (a tool echoing its input as output) is masked at depth.
	out := a.redactStepJSON(map[string]any{"echo": map[string]any{"token": "sekret-123", "topic": "hi"}})
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	echo := m["echo"].(map[string]any)
	if echo["token"] != trace.RedactedPlaceholder || echo["topic"] != "hi" {
		t.Fatalf("nested output not redacted correctly: %+v", echo)
	}

	// A scalar output (no keys to redact) is marshaled as is.
	if s := string(a.redactStepJSON("plain-output")); s != `"plain-output"` {
		t.Fatalf("scalar output should pass through, got %s", s)
	}
}

// TestRedactStepJSON_noRecorderUsesDefaults: even without a recorder, the default sensitive keys are
// applied — the write path never falls back to storing raw args.
func TestRedactStepJSON_noRecorderUsesDefaults(t *testing.T) {
	a := &engineInvoker{e: &Executor{}}
	b := a.redactStepJSON(map[string]any{"password": "p", "topic": "hi"})
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["password"] != trace.RedactedPlaceholder {
		t.Fatalf("password must be redacted even without a recorder, got %v", m["password"])
	}
}

// TestFinishRunWithOutput_redactsRunsOutput proves the runs table's final output_json is redacted
// (issue #408 follow-up): a sensitive value flowing into a workflow's output.value must not be stored
// in clear and served by inspect / state show. The final checkpoint keeps the raw context for resume.
func TestFinishRunWithOutput_redactsRunsOutput(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "fin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	runID := "r1"
	started := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{RunID: runID, WorkflowName: "demo", Env: "dev", Status: "running", StartedAt: started, InputJSON: "{}"}); err != nil {
		t.Fatal(err)
	}
	ex := &Executor{Store: st, Trace: trace.NewRecorder(st), Now: func() time.Time { return started }}
	wf := &spec.WorkflowResource{Metadata: spec.Metadata{Name: "demo"}, Spec: spec.WorkflowSpec{Steps: []spec.WorkflowStep{{ID: "a"}}}}
	ictx := Context{Input: map[string]any{}, Steps: map[string]StepResult{}}

	if err := ex.finishRunWithOutput(ctx, RunInput{RunID: runID}, wf, ictx, 0, map[string]any{"token": "sekret-123", "topic": "hi"}); err != nil {
		t.Fatalf("finishRunWithOutput: %v", err)
	}
	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(got.OutputJSON), &out); err != nil {
		t.Fatalf("run output_json not valid JSON: %v\n%s", err, got.OutputJSON)
	}
	if out["token"] != trace.RedactedPlaceholder {
		t.Fatalf("runs table output must redact token, got %v (%s)", out["token"], got.OutputJSON)
	}
	if out["topic"] != "hi" {
		t.Fatalf("non-sensitive output value must survive, got %v", out["topic"])
	}
}
