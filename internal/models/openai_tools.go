package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

// defaultOpenAIParameters is sent when ToolDef.Parameters is empty.
// OpenAI requires function.parameters to be a JSON Schema object.
var defaultOpenAIParameters = json.RawMessage(`{"type":"object","properties":{}}`)

type openaiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type openaiTool struct {
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openaiToolCallFunction `json:"function"`
}

// openaiMessage is a Chat Completions message. Content is a pointer so assistant
// tool-call turns can send JSON null when the text is empty.
type openaiMessage struct {
	Role       string           `json:"role"`
	Content    *string          `json:"content"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openaiChatRequest struct {
	Model      string          `json:"model"`
	Messages   []openaiMessage `json:"messages"`
	Tools      []openaiTool    `json:"tools,omitempty"`
	ToolChoice string          `json:"tool_choice,omitempty"`
}

type openaiChatResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   *string          `json:"content"`
			ToolCalls []openaiToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func buildOpenAIChatPayload(req GenerateRequest) ([]byte, error) {
	msgs, err := mapOpenAIMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	payload := openaiChatRequest{
		Model:    req.Model,
		Messages: msgs,
	}
	// tool_choice is only valid alongside tools; a stray ToolChoice on a
	// plain completion is ignored so existing two-field call sites stay valid.
	if len(req.Tools) > 0 {
		choice, err := mapOpenAIToolChoice(req.ToolChoiceOrDefault())
		if err != nil {
			return nil, err
		}
		tools, err := mapOpenAITools(req.Tools)
		if err != nil {
			return nil, err
		}
		payload.Tools = tools
		payload.ToolChoice = choice
	}
	return json.Marshal(payload)
}

func mapOpenAIMessages(msgs []ChatMessage) ([]openaiMessage, error) {
	out := make([]openaiMessage, 0, len(msgs))
	for _, m := range msgs {
		if len(m.ToolCalls) > 0 {
			calls, err := encodeOpenAIToolCalls(m.ToolCalls)
			if err != nil {
				return nil, err
			}
			var content *string
			if m.Content != "" {
				c := m.Content
				content = &c
			}
			role, err := openaiAssistantRole(m.Role)
			if err != nil {
				return nil, err
			}
			out = append(out, openaiMessage{Role: role, Content: content, ToolCalls: calls})
		}
		for _, r := range m.ToolResults {
			id := strings.TrimSpace(r.ToolCallID)
			if id == "" {
				return nil, fmt.Errorf("models: openai tool result missing tool_call_id")
			}
			c := r.Content
			out = append(out, openaiMessage{
				Role:       "tool",
				Content:    &c,
				ToolCallID: id,
			})
		}
		// Keep Role/Content when ToolResults is set (A1). OpenAI order is
		// assistant tool_calls, then role=tool results, then any follow-up text.
		if len(m.ToolCalls) == 0 && (len(m.ToolResults) == 0 || m.Content != "") {
			c := m.Content
			out = append(out, openaiMessage{Role: openaiTextRole(m.Role), Content: &c})
		}
	}
	return out, nil
}

func mapOpenAITools(tools []ToolDef) ([]openaiTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]openaiTool, 0, len(tools))
	for _, t := range tools {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			return nil, fmt.Errorf("models: openai tool definition missing name")
		}
		params, err := normalizeOpenAIToolParameters(t.Parameters)
		if err != nil {
			return nil, err
		}
		out = append(out, openaiTool{
			Type: "function",
			Function: openaiFunction{
				Name:        name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	return out, nil
}

func normalizeOpenAIToolParameters(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return defaultOpenAIParameters, nil
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("models: openai tool parameters are not JSON")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("models: openai tool parameters: %w", err)
	}
	if _, ok := v.(map[string]any); !ok {
		return nil, fmt.Errorf("models: openai tool parameters must be a JSON object")
	}
	return raw, nil
}

func mapOpenAIToolChoice(choice string) (string, error) {
	if choice == "" {
		choice = ToolChoiceAuto
	}
	switch choice {
	case ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired:
		return choice, nil
	default:
		return "", fmt.Errorf("models: unsupported tool_choice %q", choice)
	}
}

func openaiAssistantRole(role string) (string, error) {
	role = strings.TrimSpace(role)
	if role == "" || role == "assistant" {
		return "assistant", nil
	}
	return "", fmt.Errorf("models: openai tool_calls require assistant role, got %q", role)
}

func openaiTextRole(role string) string {
	if strings.TrimSpace(role) == "" {
		return "user"
	}
	return role
}

func encodeOpenAIToolCalls(calls []ToolCall) ([]openaiToolCall, error) {
	out := make([]openaiToolCall, 0, len(calls))
	for _, c := range calls {
		id := strings.TrimSpace(c.ID)
		name := strings.TrimSpace(c.Name)
		if id == "" {
			return nil, fmt.Errorf("models: openai tool call missing id")
		}
		if name == "" {
			return nil, fmt.Errorf("models: openai tool call %q: empty name", id)
		}
		args := "{}"
		if len(c.Arguments) > 0 {
			if !json.Valid(c.Arguments) {
				return nil, fmt.Errorf("models: openai tool call %q: arguments are not JSON", id)
			}
			args = string(c.Arguments)
		}
		out = append(out, openaiToolCall{
			ID:   id,
			Type: "function",
			Function: openaiToolCallFunction{
				Name:      name,
				Arguments: args,
			},
		})
	}
	return out, nil
}

func parseOpenAIToolCalls(raw []openaiToolCall) ([]ToolCall, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]ToolCall, 0, len(raw))
	for _, tc := range raw {
		id := strings.TrimSpace(tc.ID)
		name := strings.TrimSpace(tc.Function.Name)
		if id == "" {
			return nil, fmt.Errorf("models: openai tool call missing id")
		}
		if name == "" {
			return nil, fmt.Errorf("models: openai tool call %q: empty function name", id)
		}
		args := strings.TrimSpace(tc.Function.Arguments)
		if args == "" {
			args = "{}"
		}
		if !json.Valid([]byte(args)) {
			return nil, fmt.Errorf("models: openai tool call %q: arguments are not JSON", id)
		}
		out = append(out, ToolCall{
			ID:        id,
			Name:      name,
			Arguments: json.RawMessage(args),
		})
	}
	return out, nil
}

func mapOpenAIStopReason(finish string, nCalls int) string {
	switch finish {
	case "length":
		return StopReasonMaxTokens
	case "tool_calls":
		return StopReasonToolUse
	case "stop", "":
		// Compatible servers often send finish_reason=stop with tool_calls.
		if nCalls > 0 {
			return StopReasonToolUse
		}
		return StopReasonEndTurn
	default:
		// Preserve safety stops (content_filter) and unknown reasons even if
		// tool_calls are present — do not rewrite them to tool_use.
		return finish
	}
}

func parseOpenAIChatResponse(body []byte) (content string, calls []ToolCall, stop string, pt, ct int, err error) {
	var out openaiChatResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", nil, "", 0, 0, fmt.Errorf("models: decode openai response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", nil, "", 0, 0, fmt.Errorf("models: openai returned no choices")
	}
	ch := out.Choices[0]
	if ch.Message.Content != nil {
		content = *ch.Message.Content
	}
	calls, err = parseOpenAIToolCalls(ch.Message.ToolCalls)
	if err != nil {
		// Token-cap and safety stops often include a truncated tool_calls
		// block; surface the stop reason instead of a decode error.
		if ch.FinishReason == "length" || ch.FinishReason == "content_filter" {
			calls = nil
			err = nil
		} else {
			return "", nil, "", 0, 0, err
		}
	}
	if ch.FinishReason == "tool_calls" && len(calls) == 0 {
		return "", nil, "", 0, 0, fmt.Errorf("models: openai returned tool_calls finish without calls")
	}
	stop = mapOpenAIStopReason(ch.FinishReason, len(calls))
	if out.Usage != nil {
		pt = out.Usage.PromptTokens
		ct = out.Usage.CompletionTokens
	}
	return content, calls, stop, pt, ct, nil
}
