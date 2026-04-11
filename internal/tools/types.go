package tools

import "context"

// ToolExecutor runs one tool operation (design doc §12.2 G).
type ToolExecutor interface {
	Call(ctx context.Context, req ToolCallRequest) (ToolCallResponse, error)
}

// ToolCallRequest is a resolved workflow tool step (uses + with).
type ToolCallRequest struct {
	Uses string
	With map[string]any
}

// ToolCallResponse matches the MVP step result envelope (§13.2): output + meta.
type ToolCallResponse struct {
	Output map[string]any
	Meta   ToolCallMeta
}

// ToolCallMeta holds placeholder timing and cost (§13.2).
type ToolCallMeta struct {
	DurationMs int64
	CostUSD    float64
}
