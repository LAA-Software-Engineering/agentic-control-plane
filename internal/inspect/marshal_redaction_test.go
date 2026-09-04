package inspect

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/trace"
)

// TestCheckpointsToRecords_redactsContext is the read-layer half of the #408 fix: a run checkpoint is
// stored raw (the interpreter needs the real args to dispatch on resume), but the inspect API must not
// serve a token/password/authorization value in clear. checkpointsToRecords redacts the completed
// steps' outputs and the pending gate's args before serving, preserving structure and non-sensitive
// values.
func TestCheckpointsToRecords_redactsContext(t *testing.T) {
	ctx := `{
      "steps": {"prep": {"Output": {"echo": {"token": "sekret-123", "topic": "hi"}}}},
      "pendingHitl": {"stepId": "publish", "with": {"body": {"authorization": "Bearer abc", "note": "ok"}}}
    }`
	recs := checkpointsToRecords(
		[]state.RunCheckpoint{{Seq: 1, StepID: "publish", Status: "interrupted", ContextJSON: ctx}},
		trace.NormalizeRedactionOptions(trace.DefaultRedactionOptions()),
	)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	got := string(recs[0].Context)

	// Sensitive values are masked...
	if strings.Contains(got, "sekret-123") || strings.Contains(got, "Bearer abc") {
		t.Fatalf("checkpoint context served a sensitive value in clear:\n%s", got)
	}
	if !strings.Contains(got, trace.RedactedPlaceholder) {
		t.Fatalf("expected the redaction placeholder in the served context:\n%s", got)
	}
	// ...while non-sensitive values and structure survive.
	var m map[string]any
	if err := json.Unmarshal(recs[0].Context, &m); err != nil {
		t.Fatalf("redacted context is not valid JSON: %v\n%s", err, got)
	}
	steps := m["steps"].(map[string]any)
	prep := steps["prep"].(map[string]any)
	echo := prep["Output"].(map[string]any)["echo"].(map[string]any)
	if echo["topic"] != "hi" {
		t.Fatalf("non-sensitive step output value must survive: %+v", echo)
	}
	if echo["token"] != trace.RedactedPlaceholder {
		t.Fatalf("step output token must be redacted: %+v", echo)
	}
	pend := m["pendingHitl"].(map[string]any)["with"].(map[string]any)["body"].(map[string]any)
	if pend["note"] != "ok" || pend["authorization"] != trace.RedactedPlaceholder {
		t.Fatalf("pending gate args not redacted correctly: %+v", pend)
	}
}

// TestCheckpointsToRecords_malformedContextPassesThrough: a non-JSON context is served unchanged
// (defensive — checkpoint context is always valid JSON we wrote).
func TestCheckpointsToRecords_malformedContextPassesThrough(t *testing.T) {
	recs := checkpointsToRecords(
		[]state.RunCheckpoint{{Seq: 1, ContextJSON: "not json"}},
		trace.NormalizeRedactionOptions(trace.DefaultRedactionOptions()),
	)
	if string(recs[0].Context) != "not json" {
		t.Fatalf("malformed context should pass through, got %s", recs[0].Context)
	}
}
