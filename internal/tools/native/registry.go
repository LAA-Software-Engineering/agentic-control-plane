package native

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrUnknownOperation indicates the operation name is not implemented by this registry.
var ErrUnknownOperation = errors.New("native: unknown operation")

// ExecMeta is timing/cost metadata for a native call (§13.2).
type ExecMeta struct {
	DurationMs int64
	CostUSD    float64
}

// Registry dispatches built-in native tool operations (issue #18).
type Registry struct{}

// NewRegistry returns a registry with echo and identity operations.
func NewRegistry() *Registry {
	return &Registry{}
}

// Dispatch runs a single operation for a native-typed tool. with is the workflow step input map.
func (r *Registry) Dispatch(ctx context.Context, operation string, with map[string]any) (map[string]any, ExecMeta, error) {
	_ = ctx
	start := time.Now()
	meta := ExecMeta{CostUSD: 0}
	switch operation {
	case "echo":
		meta.DurationMs = time.Since(start).Milliseconds()
		return map[string]any{"echo": shallowCopy(with)}, meta, nil
	case "identity":
		v, ok := with["value"]
		meta.DurationMs = time.Since(start).Milliseconds()
		return map[string]any{"value": v, "ok": ok}, meta, nil
	default:
		return nil, ExecMeta{}, fmt.Errorf("%w: %q", ErrUnknownOperation, operation)
	}
}

func shallowCopy(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
