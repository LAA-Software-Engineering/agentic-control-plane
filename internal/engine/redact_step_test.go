package engine

import (
	"encoding/json"
	"testing"

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
