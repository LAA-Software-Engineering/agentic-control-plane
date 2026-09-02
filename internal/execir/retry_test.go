package execir

import (
	"context"
	"errors"
	"testing"
)

// retryProg builds `retry until s.approved limit N { s = step() }`, so a stateful respond can drive
// the success condition attempt by attempt.
func retryProg(limit int) *Program {
	return &Program{
		Workflow: "w",
		Params:   []string{"input"},
		Body: []Node{
			&Let{Bind: "s", Value: Ref{Path: []string{"input"}}},
			&Retry{Cond: Leaf{V: Ref{Path: []string{"s", "approved"}}}, Limit: limit, Body: []Node{
				&InvokeTool{Bind: "s", Uses: "step"},
			}},
		},
	}
}

func TestRetry_SucceedsOnFirstAttempt(t *testing.T) {
	rec := &recorder{respond: func(string, map[string]any) any {
		return map[string]any{"approved": true}
	}}
	if _, err := (&Interp{Invoker: rec}).Run(context.Background(), retryProg(3), map[string]any{"approved": false}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(rec.tools) != 1 {
		t.Fatalf("body ran %d times, want 1 (approved after the first attempt)", len(rec.tools))
	}
}

// The key difference from `while`: success on the FINAL attempt is detected (cond is checked after
// each body run), so an approval on attempt N is a success, not an exhaustion.
func TestRetry_SucceedsOnFinalAttempt(t *testing.T) {
	calls := 0
	rec := &recorder{respond: func(string, map[string]any) any {
		calls++
		return map[string]any{"approved": calls == 3}
	}}
	if _, err := (&Interp{Invoker: rec}).Run(context.Background(), retryProg(3), map[string]any{"approved": false}); err != nil {
		t.Fatalf("success on the final attempt must not fail: %v", err)
	}
	if len(rec.tools) != 3 {
		t.Fatalf("body ran %d times, want 3", len(rec.tools))
	}
}

// Exhausting the attempts with the condition still false is a terminal RetryExhaustedError — the
// explicit failure a bounded agent-retry loop wants, versus while's silent success.
func TestRetry_ExhaustionFailsClosed(t *testing.T) {
	rec := &recorder{respond: func(string, map[string]any) any {
		return map[string]any{"approved": false}
	}}
	_, err := (&Interp{Invoker: rec}).Run(context.Background(), retryProg(3), map[string]any{"approved": false})
	var ex *RetryExhaustedError
	if !errors.As(err, &ex) {
		t.Fatalf("exhaustion must be a RetryExhaustedError, got %v", err)
	}
	if ex.Limit != 3 {
		t.Fatalf("error Limit = %d, want 3", ex.Limit)
	}
	if len(rec.tools) != 3 {
		t.Fatalf("body ran %d times, want exactly 3 before failing", len(rec.tools))
	}
}

// A retry program round-trips through the wire encoding (serialize/deserialize) and has a stable
// digest — the pinned-program identity path (#260) must cover the new node.
func TestRetry_SerializeAndDigestRoundTrip(t *testing.T) {
	prog := retryProg(2)
	blob, err := MarshalPrograms(map[string]*Program{"w": prog})
	if err != nil {
		t.Fatal(err)
	}
	back, err := UnmarshalPrograms(blob)
	if err != nil {
		t.Fatal(err)
	}
	if back["w"].Digest() != prog.Digest() {
		t.Fatalf("digest changed across round trip: %s vs %s", back["w"].Digest(), prog.Digest())
	}
	if !RequiresInterpreter(prog) {
		t.Fatal("a program with a Retry must require the interpreter")
	}
}
