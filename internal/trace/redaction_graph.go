package trace

import (
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// RedactionFromGraph returns trace payload redaction options from project spec (issue #110).
func RedactionFromGraph(g *spec.ProjectGraph) RedactionOptions {
	opts := DefaultRedactionOptions()
	if g == nil || g.Spec.Traces == nil {
		return opts
	}
	t := g.Spec.Traces
	if len(t.RedactKeys) > 0 {
		opts.RedactKeys = append(opts.RedactKeys, t.RedactKeys...)
	}
	if t.MaxPayloadBytes > 0 {
		opts.MaxPayloadBytes = t.MaxPayloadBytes
	}
	if t.Redaction != nil {
		r := t.Redaction
		if len(r.RedactKeys) > 0 {
			opts.RedactKeys = append(opts.RedactKeys, r.RedactKeys...)
		}
		if r.MaxDepth > 0 {
			opts.MaxDepth = r.MaxDepth
		}
		if r.MaxBytes > 0 {
			opts.MaxBinaryBytes = r.MaxBytes
		}
		if r.MaxStringChars > 0 {
			opts.MaxStringChars = r.MaxStringChars
		}
		if r.MaxPayloadBytes > 0 {
			opts.MaxPayloadBytes = r.MaxPayloadBytes
		}
	}
	return NormalizeRedactionOptions(opts)
}
