package trace

import (
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestRedactionFromGraph_defaultsWhenUnset(t *testing.T) {
	t.Parallel()
	o := RedactionFromGraph(nil)
	if o.MaxDepth != defaultMaxDepth {
		t.Fatalf("MaxDepth=%d", o.MaxDepth)
	}
	if len(o.RedactKeys) == 0 {
		t.Fatal("expected default redact keys")
	}
}

func TestRedactionFromGraph_projectOverrides(t *testing.T) {
	t.Parallel()
	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Traces: &spec.ProjectTracesConfig{
				MaxPayloadBytes: 4096,
				RedactKeys:      []string{"custom_key"},
				Redaction: &spec.ProjectTracesRedactionCfg{
					MaxDepth:       10,
					MaxStringChars: 64,
				},
			},
		},
	}
	o := RedactionFromGraph(g)
	if o.MaxPayloadBytes != 4096 {
		t.Fatalf("MaxPayloadBytes=%d", o.MaxPayloadBytes)
	}
	if o.MaxDepth != 10 {
		t.Fatalf("MaxDepth=%d", o.MaxDepth)
	}
	if o.MaxStringChars != 64 {
		t.Fatalf("MaxStringChars=%d", o.MaxStringChars)
	}
	found := false
	for _, k := range o.RedactKeys {
		if k == "custom_key" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("RedactKeys=%v", o.RedactKeys)
	}
}
