package native

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
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
	start := time.Now()
	meta := ExecMeta{CostUSD: 0}
	if spec.IsShellCommandOperation(operation) {
		meta.DurationMs = time.Since(start).Milliseconds()
		cmd := spec.ExtractShellCommand(with)
		if cmd == "" {
			return nil, meta, fmt.Errorf("native: %s requires string field command, cmd, or script", operation)
		}
		return map[string]any{"command": cmd}, meta, nil
	}
	handler, ok := dispatchHandlers[operation]
	if !ok {
		return nil, ExecMeta{}, fmt.Errorf("%w: %q", ErrUnknownOperation, operation)
	}
	return handler(ctx, with, start)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
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
