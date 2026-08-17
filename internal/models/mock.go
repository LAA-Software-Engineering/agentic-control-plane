package models

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// MockTurn is one scripted Generate response for a tool-calling loop (issue #159).
// StopReason defaults to [StopReasonToolUse] when ToolCalls is non-empty, otherwise [StopReasonEndTurn].
type MockTurn struct {
	Content    string
	ToolCalls  []ToolCall
	StopReason string
	// Meta is per-call accounting (token counts, cost). When nil, [MockClient.Meta] or a small default is used.
	// Token counts with CostUSD 0 are priced via the B1 table for req.Model (issue #164).
	Meta *GenerateMeta
	// Err, when set, is returned from Generate instead of a response.
	Err error
}

// MockClient returns deterministic output for tests and offline agent steps (design doc §12.2 F MVP).
//
// When Script is empty, every Generate returns Content with Meta (legacy single-shot behavior)
// and [StopReasonEndTurn], unless req.Tools includes a restart-like tool (issue #167): then the
// first Generate returns [StopReasonToolUse] for that tool and the next returns status JSON.
// When Script is set, each Generate consumes the next turn. After the script is exhausted,
// Generate returns an error so extra loop iterations fail in tests.
//
// Each call records the request (including Tools) so tests can assert on what the loop sent.
// [MockClient.Reset] clears the cursor, recorded requests, and the restart hook when a test reuses one client.
type MockClient struct {
	Content string
	Meta    *GenerateMeta
	Script  []MockTurn

	mu       sync.Mutex
	call     int
	requests []GenerateRequest
	// restartHookFired is set after the empty-Script restart tool_use so the follow-up
	// Generate returns JSON Content (issue #167).
	restartHookFired bool
}

// Generate returns the next scripted turn, or the fixed Content when Script is empty.
func (m *MockClient) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests = append(m.requests, cloneGenerateRequest(req))

	if len(m.Script) == 0 {
		if name := restartLikeToolName(req.Tools); name != "" {
			if !m.restartHookFired {
				m.restartHookFired = true
				return GenerateResponse{
					ToolCalls: []ToolCall{{
						ID:        "call_restart",
						Name:      name,
						Arguments: json.RawMessage(`{}`),
					}},
					StopReason: StopReasonToolUse,
					Meta:       m.metaFor(req.Model, nil),
				}, nil
			}
			return GenerateResponse{
				Content:    mockRestartFollowUpJSON,
				StopReason: StopReasonEndTurn,
				Meta:       m.metaFor(req.Model, nil),
			}, nil
		}
		return GenerateResponse{
			Content:    m.Content,
			StopReason: StopReasonEndTurn,
			Meta:       m.metaFor(req.Model, nil),
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
		ToolCalls:  cloneToolCalls(turn.ToolCalls),
		StopReason: stop,
		Meta:       m.metaFor(req.Model, turn.Meta),
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

// Reset clears recorded requests, the script cursor, and the empty-Script restart hook so one
// client can be reused across cases. Content, Meta, and Script are left unchanged.
func (m *MockClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.call = 0
	m.requests = nil
	m.restartHookFired = false
}

func (m *MockClient) metaFor(model string, turn *GenerateMeta) GenerateMeta {
	var meta GenerateMeta
	switch {
	case turn != nil:
		meta = *turn
	case m != nil && m.Meta != nil:
		meta = *m.Meta
	default:
		return GenerateMeta{DurationMs: 1, CostUSD: 0.001}
	}
	if meta.CostUSD == 0 && (meta.PromptTokens != 0 || meta.CompletionTokens != 0) {
		meta.CostUSD = estimateMockTokenCostUSD(model, meta.PromptTokens, meta.CompletionTokens)
	}
	return meta
}

// estimateMockTokenCostUSD uses the same B1 tables as live adapters (issue #164).
// An explicit non-zero CostUSD on Meta is left unchanged by [MockClient.metaFor].
func estimateMockTokenCostUSD(model string, promptTokens, completionTokens int) float64 {
	id := strings.TrimSpace(model)
	if i := strings.IndexByte(id, '/'); i >= 0 && i < len(id)-1 {
		id = id[i+1:]
	}
	if c := estimateOpenAIChatCostUSD(id, promptTokens, completionTokens); c != 0 {
		return c
	}
	return estimateAnthropicCostUSD(id, promptTokens, completionTokens)
}

func cloneGenerateRequest(req GenerateRequest) GenerateRequest {
	out := req
	if req.Messages != nil {
		out.Messages = make([]ChatMessage, len(req.Messages))
		copy(out.Messages, req.Messages)
		for i, msg := range out.Messages {
			out.Messages[i].ToolCalls = cloneToolCalls(msg.ToolCalls)
			if msg.ToolResults != nil {
				out.Messages[i].ToolResults = append([]ToolResult(nil), msg.ToolResults...)
			}
		}
	}
	out.Tools = cloneToolDefs(req.Tools)
	return out
}

func cloneToolDefs(tools []ToolDef) []ToolDef {
	if tools == nil {
		return nil
	}
	out := make([]ToolDef, len(tools))
	copy(out, tools)
	for i := range out {
		out[i].Parameters = cloneRawMessage(out[i].Parameters)
	}
	return out
}

func cloneToolCalls(calls []ToolCall) []ToolCall {
	if calls == nil {
		return nil
	}
	out := make([]ToolCall, len(calls))
	copy(out, calls)
	for i := range out {
		out[i].Arguments = cloneRawMessage(out[i].Arguments)
	}
	return out
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	out := make(json.RawMessage, len(raw))
	copy(out, raw)
	return out
}

// mockRestartFollowUpJSON is the second-turn Content after the empty-Script restart hook
// (issue #167). It is independent of [MockClient.Content] so Registry's PR-review JSON
// does not fail the incident-triage output schema.
const mockRestartFollowUpJSON = `{"summary":"Restart requested after correlating pager alert with error logs.","severity":"high","action":"restart"}`

// restartLikeToolName returns the first advertised tool whose name is "restart" or contains
// "restart" (ASCII case-folding). Empty Script uses this to drive a gated remediation call.
func restartLikeToolName(tools []ToolDef) string {
	for _, t := range tools {
		n := strings.ToLower(strings.TrimSpace(t.Name))
		if n == "restart" || strings.Contains(n, "restart") {
			return t.Name
		}
	}
	return ""
}
