package engine

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Terfyn/terfyn/internal/trace"
)

// TestTruncateMapInPlace_keepsValidUTF8 proves the truncated tool input stays valid
// UTF-8 (rune-boundary cuts) rather than being split mid-rune (#386). The truncated
// value is what gets dispatched to the tool, so a mid-rune cut corrupts it.
func TestTruncateMapInPlace_keepsValidUTF8(t *testing.T) {
	s := strings.Repeat("日本語", 70)
	in := map[string]any{"text": s}
	out, _, truncated, err := truncateMapInPlace(in, 64, trace.DefaultRedactionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatal("expected truncation")
	}
	got, _ := out["text"].(string)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated tool input is not valid UTF-8: %q", got)
	}
}

// TestTruncateRunes_runeBoundaries checks the head/tail cuts land on rune boundaries
// across a range of byte budgets over multi-byte text.
func TestTruncateRunes_runeBoundaries(t *testing.T) {
	s := strings.Repeat("🙂", 40) // 4 bytes each
	for max := 1; max <= len(s); max++ {
		got := truncateRunes(s, max)
		if !utf8.ValidString(got) {
			t.Fatalf("truncateRunes(max=%d) invalid UTF-8: %q", max, got)
		}
		if len(got) > max && max < len(s) {
			t.Fatalf("truncateRunes(max=%d) exceeded budget: %d bytes", max, len(got))
		}
	}
}

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
