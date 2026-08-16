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
	if len(req.Tools) > 0 {
		choice, err := mapOpenAIToolChoice(req.ToolChoiceOrDefault())
		if err != nil {
			return nil, err
		}
		payload.Tools = mapOpenAITools(req.Tools)
		payload.ToolChoice = choice
	}
	return json.Marshal(payload)
}

func mapOpenAIMessages(msgs []ChatMessage) ([]openaiMessage, error) {
	out := make([]openaiMessage, 0, len(msgs))
	for _, m := range msgs {
		switch {
		case len(m.ToolCalls) > 0:
			calls, err := encodeOpenAIToolCalls(m.ToolCalls)
			if err != nil {
				return nil, err
			}
			var content *string
			if m.Content != "" {
				c := m.Content
				content = &c
			}
			role := m.Role
			if strings.TrimSpace(role) == "" {
				role = "assistant"
			}
			out = append(out, openaiMessage{Role: role, Content: content, ToolCalls: calls})
		case len(m.ToolResults) == 0:
			c := m.Content
			out = append(out, openaiMessage{Role: m.Role, Content: &c})
		}
		for _, r := range m.ToolResults {
			c := r.Content
			out = append(out, openaiMessage{
				Role:       "tool",
				Content:    &c,
				ToolCallID: r.ToolCallID,
			})
		}
	}
	return out, nil
}

func mapOpenAITools(tools []ToolDef) []openaiTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openaiTool, 0, len(tools))
	for _, t := range tools {
		params := t.Parameters
		if len(params) == 0 {
			params = defaultOpenAIParameters
		}
		out = append(out, openaiTool{
			Type: "function",
			Function: openaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	return out
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

func encodeOpenAIToolCalls(calls []ToolCall) ([]openaiToolCall, error) {
	out := make([]openaiToolCall, 0, len(calls))
	for _, c := range calls {
		args := "{}"
		if len(c.Arguments) > 0 {
			if !json.Valid(c.Arguments) {
				return nil, fmt.Errorf("models: openai tool call %q: arguments are not JSON", c.ID)
			}
			args = string(c.Arguments)
		}
		out = append(out, openaiToolCall{
			ID:   c.ID,
			Type: "function",
			Function: openaiToolCallFunction{
				Name:      c.Name,
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
	var out []ToolCall
	for _, tc := range raw {
		if tc.Function.Name == "" {
			continue
		}
		args := strings.TrimSpace(tc.Function.Arguments)
		if !json.Valid([]byte(args)) {
			id := tc.ID
			if id == "" {
				id = tc.Function.Name
			}
			return nil, fmt.Errorf("models: openai tool call %q: arguments are not JSON", id)
		}
		out = append(out, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(args),
		})
	}
	return out, nil
}

func mapOpenAIStopReason(finish string, nCalls int) string {
	switch finish {
	case "tool_calls":
		return StopReasonToolUse
	case "stop":
		return StopReasonEndTurn
	case "length":
		return StopReasonMaxTokens
	case "":
		if nCalls > 0 {
			return StopReasonToolUse
		}
		return StopReasonEndTurn
	default:
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
		return "", nil, "", 0, 0, err
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
