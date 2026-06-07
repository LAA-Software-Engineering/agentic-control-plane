package trace

import (
	"encoding/json"
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// LimitHitEventData is the canonical payload for [EventLimitHit] (issue #117).
type LimitHitEventData struct {
	Kind          spec.LimitKind         `json:"kind"`
	MaxBytes      int                    `json:"maxBytes"`
	OriginalBytes int                    `json:"originalBytes"`
	Policy        spec.LimitExceedPolicy `json:"policy"`
	Truncated     bool                   `json:"truncated,omitempty"`
	StepID        string                 `json:"stepId,omitempty"`
	Uses          string                 `json:"uses,omitempty"`
}

// LimitHitTraceData returns JSON-safe event data for a limit evaluation.
func LimitHitTraceData(kind spec.LimitKind, maxBytes, originalBytes int, policy spec.LimitExceedPolicy, truncated bool, stepID, uses string) map[string]any {
	d := LimitHitEventData{
		Kind:          kind,
		MaxBytes:      maxBytes,
		OriginalBytes: originalBytes,
		Policy:        policy,
		Truncated:     truncated,
		StepID:        stepID,
		Uses:          uses,
	}
	b, err := json.Marshal(d)
	if err != nil {
		return map[string]any{
			"kind":          string(kind),
			"maxBytes":      maxBytes,
			"originalBytes": originalBytes,
			"policy":        string(policy),
			"truncated":     truncated,
		}
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]any{"kind": string(kind)}
	}
	return out
}

// TruncateMapValue shortens v when its stable JSON encoding exceeds maxBytes.
// When truncated, the returned map uses [FieldPayloadTruncated] and [FieldPayloadPreview]
// consistent with issue #110 trace truncation. Use for trace/event payloads only;
// live tool I/O uses engine in-place truncation instead.
func TruncateMapValue(v map[string]any, maxBytes int, opts RedactionOptions) (map[string]any, int, bool, error) {
	if v == nil {
		v = map[string]any{}
	}
	b, err := render.MarshalStableJSON(v)
	if err != nil {
		return nil, 0, false, fmt.Errorf("trace: marshal value for limit: %w", err)
	}
	orig := len(b)
	if maxBytes <= 0 || orig <= maxBytes {
		return v, orig, false, nil
	}
	o := opts.normalized()
	o.MaxPayloadBytes = maxBytes
	truncated := PrepareEventData(map[string]any{FieldWrappedValue: v}, nil, o)
	out := map[string]any{
		FieldPayloadTruncated: true,
		FieldPayloadPreview:   truncated,
		"originalBytes":       orig,
		"maxBytes":            maxBytes,
	}
	return out, orig, true, nil
}

// JSONByteLen returns the stable JSON byte length of v.
func JSONByteLen(v any) (int, error) {
	b, err := render.MarshalStableJSON(v)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}
