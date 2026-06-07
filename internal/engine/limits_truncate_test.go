package engine

import (
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

func TestTruncateMapInPlace_preservesTopLevelKeys(t *testing.T) {
	t.Parallel()
	in := map[string]any{
		"echo": map[string]any{
			"topic": "agents",
			"extra": strings.Repeat("x", 500),
		},
	}
	out, orig, truncated, err := truncateMapInPlace(in, 80, trace.DefaultRedactionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatal("expected truncation")
	}
	if orig <= 80 {
		t.Fatalf("orig = %d", orig)
	}
	echo, ok := out["echo"].(map[string]any)
	if !ok {
		t.Fatalf("echo key missing or wrong type: %v", out)
	}
	if echo["topic"] != "agents" {
		t.Fatalf("topic = %v", echo["topic"])
	}
	n, err := stableJSONLen(out)
	if err != nil {
		t.Fatal(err)
	}
	if n > 80 {
		t.Fatalf("still over limit: %d", n)
	}
	if _, ok := out[trace.FieldPayloadTruncated]; ok {
		t.Fatal("must not use trace envelope fields")
	}
}

func TestTruncateMapInPlace_underLimitUnchanged(t *testing.T) {
	t.Parallel()
	in := map[string]any{"a": "b"}
	out, _, truncated, err := truncateMapInPlace(in, 100, trace.DefaultRedactionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("unexpected truncation")
	}
	if out["a"] != "b" {
		t.Fatalf("out = %v", out)
	}
}
