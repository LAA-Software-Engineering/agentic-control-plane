package tools

import "context"

// MockExecutor returns a fixed response (or Fn) for tests.
type MockExecutor struct {
	Resp ToolCallResponse
	Err  error
	Fn   func(ctx context.Context, req ToolCallRequest) (ToolCallResponse, error)
}

// Call implements [ToolExecutor].
func (m *MockExecutor) Call(ctx context.Context, req ToolCallRequest) (ToolCallResponse, error) {
	if m.Fn != nil {
		return m.Fn(ctx, req)
	}
	if m.Err != nil {
		return ToolCallResponse{}, m.Err
	}
	out := m.Resp.Output
	if out == nil {
		out = map[string]any{}
	}
	return ToolCallResponse{Output: out, Meta: m.Resp.Meta}, nil
}
