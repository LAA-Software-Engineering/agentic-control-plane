package models

import (
	"context"
	"encoding/json"
)

// ModelClient invokes a chat-capable model (design doc §12.2 F).
type ModelClient interface {
	Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
}

// Tool choice constants for [GenerateRequest.ToolChoice].
// The zero value of ToolChoice behaves as [ToolChoiceAuto].
const (
	ToolChoiceAuto     = "auto"
	ToolChoiceNone     = "none"
	ToolChoiceRequired = "required"
)

// Stop reason constants for [GenerateResponse.StopReason].
const (
	StopReasonEndTurn   = "end_turn"
	StopReasonToolUse   = "tool_use"
	StopReasonMaxTokens = "max_tokens"
)

// ToolDef describes one callable tool exposed to the model (provider-neutral JSON Schema parameters).
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall is a model-issued request to invoke a tool before the turn ends.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult carries one tool execution result back to the model on a follow-up turn.
// Attach results on [ChatMessage] via [ChatMessage.ToolResults]; keep [ChatMessage.Role] and
// [ChatMessage.Content] for ordinary text turns. Providers map this field to their native
// tool-result blocks in adapter code (issue #156).
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
}

// ChatMessage is one turn in the prompt payload.
type ChatMessage struct {
	Role        string       `json:"role"`
	Content     string       `json:"content,omitempty"`
	ToolResults []ToolResult `json:"tool_results,omitempty"`
}

// GenerateRequest is a generation call with optional tool definitions.
type GenerateRequest struct {
	Model      string        `json:"model"`
	Messages   []ChatMessage `json:"messages"`
	Tools      []ToolDef     `json:"tools,omitempty"`
	ToolChoice string        `json:"tool_choice,omitempty"`
}

// ToolChoiceOrDefault returns [ToolChoiceAuto] when ToolChoice is unset.
func (r GenerateRequest) ToolChoiceOrDefault() string {
	if r.ToolChoice == "" {
		return ToolChoiceAuto
	}
	return r.ToolChoice
}

// GenerateResponse carries model output, optional tool calls, and accounting metadata.
type GenerateResponse struct {
	// Content is assistant message text when the model replies directly.
	Content    string       `json:"content,omitempty"`
	ToolCalls  []ToolCall   `json:"tool_calls,omitempty"`
	StopReason string       `json:"stop_reason,omitempty"`
	Meta       GenerateMeta `json:"meta"`
}

// GenerateMeta holds duration, token usage, and cost accounting (§13.2 style).
// CostUSD is 0 for the mock client unless injected. For OpenAI chat completions it is a rough
// estimate from usage × published per-million token rates when the model is recognized.
type GenerateMeta struct {
	DurationMs       int64   `json:"duration_ms"`
	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
}
