package engine

import (
	"encoding/json"
	"testing"
)

func TestMarshalCheckpointPayload_stableKeyOrder(t *testing.T) {
	ictx := Context{
		Input: map[string]any{"b": 2, "a": 1},
		Steps: map[string]StepResult{
			"z": {Output: map[string]any{"x": 1}, Meta: map[string]any{"costUsd": 0.1}},
		},
	}
	s1, err := marshalCheckpointPayload(ictx, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := marshalCheckpointPayload(ictx, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if s1 != s2 {
		t.Fatalf("non-deterministic: %q vs %q", s1, s2)
	}
	if !json.Valid([]byte(s1)) {
		t.Fatalf("invalid json: %s", s1)
	}

	gotCtx, cost, err := unmarshalCheckpointPayload(s1)
	if err != nil {
		t.Fatal(err)
	}
	if cost != 0.5 {
		t.Fatalf("cost = %v", cost)
	}
	b, _ := json.Marshal(gotCtx.Input)
	if string(b) != `{"a":1,"b":2}` && string(b) != `{"b":2,"a":1}` {
		t.Fatalf("input round-trip %s", b)
	}
	if len(gotCtx.Steps) != 1 {
		t.Fatalf("steps = %d", len(gotCtx.Steps))
	}
}

func TestUnmarshalCheckpointPayload_malformed(t *testing.T) {
	_, _, err := unmarshalCheckpointPayload(`not-json`)
	if err == nil {
		t.Fatal("expected error")
	}
}
