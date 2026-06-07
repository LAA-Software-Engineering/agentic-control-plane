package trace

import (
	"encoding/json"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestLimitHitTraceData_golden(t *testing.T) {
	t.Parallel()
	got := LimitHitTraceData(
		spec.LimitKindToolOutput,
		256000,
		300000,
		spec.LimitExceedTruncate,
		true,
		"fetch",
		"tool.helper.echo",
	)
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"kind":"tool_output","maxBytes":256000,"originalBytes":300000,"policy":"truncate","stepId":"fetch","truncated":true,"uses":"tool.helper.echo"}`
	if string(b) != want {
		t.Fatalf("golden mismatch:\ngot  %s\nwant %s", b, want)
	}
}

func TestTruncateMapValue_boundary(t *testing.T) {
	t.Parallel()
	small := map[string]any{"x": "hi"}
	out, orig, truncated, err := TruncateMapValue(small, 100, DefaultRedactionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("should not truncate under limit")
	}
	if orig <= 0 {
		t.Fatalf("orig = %d", orig)
	}
	if out["x"] != "hi" {
		t.Fatalf("out = %v", out)
	}
}

func TestTruncateMapValue_overBoundary(t *testing.T) {
	t.Parallel()
	large := map[string]any{"blob": string(make([]byte, 500))}
	out, orig, truncated, err := TruncateMapValue(large, 40, DefaultRedactionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatal("expected truncation")
	}
	if orig <= 40 {
		t.Fatalf("orig = %d", orig)
	}
	if out[FieldPayloadTruncated] != true {
		t.Fatalf("missing truncation marker: %v", out)
	}
	if _, ok := out[FieldPayloadPreview].(map[string]any); !ok {
		t.Fatalf("preview missing: %v", out)
	}
}

func TestEventLimitHit_known(t *testing.T) {
	t.Parallel()
	if err := ValidateEventType(EventLimitHit); err != nil {
		t.Fatal(err)
	}
}

func TestLimitHitTraceData_failPolicy(t *testing.T) {
	t.Parallel()
	got := LimitHitTraceData(
		spec.LimitKindCheckpoint,
		1000000,
		1500000,
		spec.LimitExceedFail,
		false,
		"",
		"",
	)
	if _, ok := got["truncated"]; ok {
		t.Fatalf("truncated should be omitted when false, got %v", got["truncated"])
	}
	if got["policy"] != string(spec.LimitExceedFail) {
		t.Fatalf("policy = %v", got["policy"])
	}
}
