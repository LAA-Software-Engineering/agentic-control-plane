package models

import (
	"context"
	"fmt"
	"sync"
)

// MockTurn is one scripted Generate response for a tool-calling loop (issue #159).
// StopReason defaults to [StopReasonToolUse] when ToolCalls is non-empty, otherwise [StopReasonEndTurn].
type MockTurn struct {
	Content    string
	ToolCalls  []ToolCall
	StopReason string
	// Meta is per-call accounting (token counts, cost). When nil, [MockClient.Meta] or a small default is used.
	Meta *GenerateMeta
	// Err, when set, is returned from Generate instead of a response.
	Err error
}

// MockClient returns deterministic output for tests and offline agent steps (design doc §12.2 F MVP).
//
// When Script is empty, every Generate returns Content with Meta (legacy single-shot behavior)
// and [StopReasonEndTurn]. When Script is set, each Generate consumes the next turn. After the
// script is exhausted, Generate returns an error so extra loop iterations fail in tests.
//
// Each call records the request (including Tools) so tests can assert on what the loop sent.
type MockClient struct {
	Content string
	Meta    *GenerateMeta
	Script  []MockTurn

	mu       sync.Mutex
	call     int
	requests []GenerateRequest
}

// Generate returns the next scripted turn, or the fixed Content when Script is empty.
func (m *MockClient) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests = append(m.requests, cloneGenerateRequest(req))

	if len(m.Script) == 0 {
		return GenerateResponse{
			Content:    m.Content,
			StopReason: StopReasonEndTurn,
			Meta:       m.metaFor(nil),
		}, nil
	}
	if m.call >= len(m.Script) {
		return GenerateResponse{}, fmt.Errorf("models: mock script exhausted after %d call(s)", len(m.Script))
	}
	turn := m.Script[m.call]
	m.call++
	if turn.Err != nil {
		return GenerateResponse{}, turn.Err
	}
	stop := turn.StopReason
	if stop == "" {
		if len(turn.ToolCalls) > 0 {
			stop = StopReasonToolUse
		} else {
			stop = StopReasonEndTurn
		}
	}
	return GenerateResponse{
		Content:    turn.Content,
		ToolCalls:  append([]ToolCall(nil), turn.ToolCalls...),
		StopReason: stop,
		Meta:       m.metaFor(turn.Meta),
	}, nil
}

// Requests returns a copy of every GenerateRequest received, in call order.
func (m *MockClient) Requests() []GenerateRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]GenerateRequest, len(m.requests))
	for i, req := range m.requests {
		out[i] = cloneGenerateRequest(req)
	}
	return out
}

// LastRequest returns the most recent GenerateRequest, or a zero value if Generate has not been called.
func (m *MockClient) LastRequest() GenerateRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) == 0 {
		return GenerateRequest{}
	}
	return cloneGenerateRequest(m.requests[len(m.requests)-1])
}

// CallCount returns how many times Generate has been invoked.
func (m *MockClient) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

func (m *MockClient) metaFor(turn *GenerateMeta) GenerateMeta {
	if turn != nil {
		return *turn
	}
	if m.Meta != nil {
		return *m.Meta
	}
	return GenerateMeta{DurationMs: 1, CostUSD: 0.001}
}

func cloneGenerateRequest(req GenerateRequest) GenerateRequest {
	out := req
	if req.Messages != nil {
		out.Messages = make([]ChatMessage, len(req.Messages))
		copy(out.Messages, req.Messages)
		for i, msg := range out.Messages {
			if msg.ToolCalls != nil {
				out.Messages[i].ToolCalls = append([]ToolCall(nil), msg.ToolCalls...)
			}
			if msg.ToolResults != nil {
				out.Messages[i].ToolResults = append([]ToolResult(nil), msg.ToolResults...)
			}
		}
	}
	if req.Tools != nil {
		out.Tools = append([]ToolDef(nil), req.Tools...)
	}
	return out
}
