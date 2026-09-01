package models

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/models/anthropic"
)

func TestMapAnthropicToolChoice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		given   string
		want    string
		wantErr string
	}{
		{name: "empty is auto", given: "", want: "auto"},
		{name: "auto", given: ToolChoiceAuto, want: "auto"},
		{name: "none", given: ToolChoiceNone, want: "none"},
		{name: "required maps to any", given: ToolChoiceRequired, want: "any"},
		{name: "unsupported", given: "force_foo", wantErr: `unsupported tool_choice "force_foo"`},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := mapAnthropicToolChoice(tc.given)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || got.Type != tc.want {
				t.Fatalf("got %+v want type %q", got, tc.want)
			}
		})
	}
}

func TestMapToAnthropicRequest_toolsAndChoice(t *testing.T) {
	t.Parallel()
	got, err := mapToAnthropicRequest(GenerateRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []ChatMessage{
			{Role: "user", Content: "weather?"},
		},
		Tools: []ToolDef{
			{
				Name:        "get_weather",
				Description: "City weather",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ToolChoice == nil || got.ToolChoice.Type != "auto" {
		t.Fatalf("tool_choice %+v", got.ToolChoice)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "get_weather" {
		t.Fatalf("tools %+v", got.Tools)
	}
	if string(got.Tools[0].InputSchema) != `{"type":"object"}` {
		t.Fatalf("input_schema %s", got.Tools[0].InputSchema)
	}
}

func TestMapToAnthropicRequest_omitsToolsWhenEmpty(t *testing.T) {
	t.Parallel()
	got, err := mapToAnthropicRequest(GenerateRequest{
		Model:      "claude-sonnet-4-20250514",
		Messages:   []ChatMessage{{Role: "user", Content: "hi"}},
		ToolChoice: ToolChoiceNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ToolChoice != nil || len(got.Tools) != 0 {
		t.Fatalf("tools/choice should be omitted: %+v", got)
	}
}

func TestMapToAnthropicRequest_emptyParametersDefault(t *testing.T) {
	t.Parallel()
	got, err := mapToAnthropicRequest(GenerateRequest{
		Model:    "m",
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
		Tools:    []ToolDef{{Name: "noop"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Tools[0].InputSchema) != `{"type":"object","properties":{}}` {
		t.Fatalf("input_schema %s", got.Tools[0].InputSchema)
	}
}

func TestMapToAnthropicRequest_rejectsNonObjectParameters(t *testing.T) {
	t.Parallel()
	_, err := mapToAnthropicRequest(GenerateRequest{
		Model:    "m",
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
		Tools:    []ToolDef{{Name: "noop", Parameters: json.RawMessage(`[]`)}},
	})
	if err == nil || !strings.Contains(err.Error(), "must be a JSON object") {
		t.Fatalf("got %v", err)
	}
}

func TestMapAnthropicMessages_toolResultsRoundTrip(t *testing.T) {
	t.Parallel()
	_, msgs, err := mapAnthropicMessages([]ChatMessage{
		{Role: "user", Content: "weather in Paris?"},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "toolu_abc123", Name: "get_weather", Arguments: json.RawMessage(`{"city":"Paris"}`)},
			},
		},
		{
			Role: "user",
			ToolResults: []ToolResult{
				{ToolCallID: "toolu_abc123", Content: `{"temp_c":18}`},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len=%d msgs=%+v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || msgs[0].Content != "weather in Paris?" || len(msgs[0].Blocks) != 0 {
		t.Fatalf("user %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || len(msgs[1].Blocks) != 1 || msgs[1].Blocks[0].Type != "tool_use" {
		t.Fatalf("assistant %+v", msgs[1])
	}
	if msgs[1].Blocks[0].ID != "toolu_abc123" || string(msgs[1].Blocks[0].Input) != `{"city":"Paris"}` {
		t.Fatalf("tool_use %+v", msgs[1].Blocks)
	}
	if msgs[2].Role != "user" || len(msgs[2].Blocks) != 1 || msgs[2].Blocks[0].Type != "tool_result" {
		t.Fatalf("tool_result %+v", msgs[2])
	}
	if msgs[2].Blocks[0].ToolUseID != "toolu_abc123" || msgs[2].Blocks[0].Content != `{"temp_c":18}` {
		t.Fatalf("tool_result %+v", msgs[2].Blocks)
	}
}

func TestMapAnthropicMessages_mergesConsecutiveToolResults(t *testing.T) {
	t.Parallel()
	_, msgs, err := mapAnthropicMessages([]ChatMessage{
		{Role: "user", ToolResults: []ToolResult{{ToolCallID: "toolu_1", Content: "a"}}},
		{Role: "user", ToolResults: []ToolResult{{ToolCallID: "toolu_2", Content: ""}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Role != "user" || len(msgs[0].Blocks) != 2 {
		t.Fatalf("msgs %+v", msgs)
	}
	if msgs[0].Blocks[0].ToolUseID != "toolu_1" || msgs[0].Blocks[0].Content != "a" {
		t.Fatalf("first %+v", msgs[0].Blocks[0])
	}
	if msgs[0].Blocks[1].ToolUseID != "toolu_2" || msgs[0].Blocks[1].Content != "" {
		t.Fatalf("second %+v", msgs[0].Blocks[1])
	}
}

func TestMapAnthropicMessages_mergesUserTextThenToolResults(t *testing.T) {
	t.Parallel()
	_, msgs, err := mapAnthropicMessages([]ChatMessage{
		{Role: "user", Content: "also consider this"},
		{Role: "user", ToolResults: []ToolResult{{ToolCallID: "toolu_1", Content: "ok"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Role != "user" || len(msgs[0].Blocks) != 2 {
		t.Fatalf("msgs %+v", msgs)
	}
	if msgs[0].Blocks[0].Type != "text" || msgs[0].Blocks[0].Text != "also consider this" {
		t.Fatalf("text %+v", msgs[0].Blocks[0])
	}
	if msgs[0].Blocks[1].Type != "tool_result" || msgs[0].Blocks[1].ToolUseID != "toolu_1" {
		t.Fatalf("tool_result %+v", msgs[0].Blocks[1])
	}
}

func TestMapAnthropicMessages_contentAndToolResults(t *testing.T) {
	t.Parallel()
	_, msgs, err := mapAnthropicMessages([]ChatMessage{
		{
			Role:    "user",
			Content: "use that result",
			ToolResults: []ToolResult{
				{ToolCallID: "toolu_abc123", Content: `{"temp_c":18}`},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len=%d msgs=%+v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || len(msgs[0].Blocks) != 2 {
		t.Fatalf("user %+v", msgs[0])
	}
	if msgs[0].Blocks[0].Type != "tool_result" || msgs[0].Blocks[0].ToolUseID != "toolu_abc123" {
		t.Fatalf("tool_result %+v", msgs[0].Blocks[0])
	}
	if msgs[0].Blocks[1].Type != "text" || msgs[0].Blocks[1].Text != "use that result" {
		t.Fatalf("text %+v", msgs[0].Blocks[1])
	}
}

func TestMapAnthropicMessages_rejectsNonAssistantToolCalls(t *testing.T) {
	t.Parallel()
	_, _, err := mapAnthropicMessages([]ChatMessage{
		{Role: "user", ToolCalls: []ToolCall{{ID: "c1", Name: "x", Arguments: json.RawMessage(`{}`)}}},
	})
	if err == nil || !strings.Contains(err.Error(), "requires assistant role") {
		t.Fatalf("got %v", err)
	}
}

func TestMapAnthropicMessages_emptyFollowUpRoleDefaultsUser(t *testing.T) {
	t.Parallel()
	_, msgs, err := mapAnthropicMessages([]ChatMessage{
		{
			Content: "continue",
			ToolResults: []ToolResult{
				{ToolCallID: "call_1", Content: "ok"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Role != "user" || len(msgs[0].Blocks) != 2 {
		t.Fatalf("msgs %+v", msgs)
	}
	if msgs[0].Blocks[1].Text != "continue" {
		t.Fatalf("text %+v", msgs[0].Blocks)
	}
}

func TestMapAnthropicMessages_emptyToolCallID(t *testing.T) {
	t.Parallel()
	_, _, err := mapAnthropicMessages([]ChatMessage{
		{ToolResults: []ToolResult{{Content: "ok"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "missing tool_call_id") {
		t.Fatalf("got %v", err)
	}
}

func TestMapAnthropicMessages_systemAndUnknownRole(t *testing.T) {
	t.Parallel()
	sys, msgs, err := mapAnthropicMessages([]ChatMessage{
		{Role: "system", Content: "Be brief."},
		{Role: "user", Content: "hi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sys != "Be brief." || len(msgs) != 1 || msgs[0].Role != "user" {
		t.Fatalf("sys=%q msgs=%+v", sys, msgs)
	}
	_, _, err = mapAnthropicMessages([]ChatMessage{{Role: "tool", Content: "nope"}})
	if err == nil || !strings.Contains(err.Error(), `does not support message role "tool"`) {
		t.Fatalf("got %v", err)
	}
}

func TestEncodeAnthropicToolUse_requiresIDAndName(t *testing.T) {
	t.Parallel()
	_, err := encodeAnthropicToolUse("", []ToolCall{{Name: "get_weather", Arguments: json.RawMessage(`{}`)}})
	if err == nil || !strings.Contains(err.Error(), "missing id") {
		t.Fatalf("id err %v", err)
	}
	_, err = encodeAnthropicToolUse("", []ToolCall{{ID: "toolu_1"}})
	if err == nil || !strings.Contains(err.Error(), "empty name") {
		t.Fatalf("name err %v", err)
	}
}

func TestEncodeAnthropicToolUse_trimsIDAndName(t *testing.T) {
	t.Parallel()
	got, err := encodeAnthropicToolUse("checking", []ToolCall{
		{ID: "  toolu_1  ", Name: "  get_weather  ", Arguments: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Type != "text" || got[0].Text != "checking" {
		t.Fatalf("preamble %+v", got)
	}
	if got[1].ID != "toolu_1" || got[1].Name != "get_weather" {
		t.Fatalf("got %+v", got[1])
	}
}

func TestAnthropicTextRole_trims(t *testing.T) {
	t.Parallel()
	got, err := anthropicTextRole("user")
	if err != nil || got != "user" {
		t.Fatalf("got %q %v", got, err)
	}
	got, err = anthropicTextRole("")
	if err != nil || got != "user" {
		t.Fatalf("whitespace got %q %v", got, err)
	}
	got, err = anthropicTextRole("assistant")
	if err != nil || got != "assistant" {
		t.Fatalf("assistant got %q %v", got, err)
	}
}

func TestMapFromAnthropicResponse_copiesUsageAndCalls(t *testing.T) {
	t.Parallel()
	got := mapFromAnthropicResponse(anthropic.Response{
		Text:         "hi",
		StopReason:   StopReasonToolUse,
		InputTokens:  3,
		OutputTokens: 4,
		DurationMs:   9,
		ToolCalls: []anthropic.ToolUse{
			{ID: "toolu_1", Name: "search", Input: json.RawMessage(`{"q":"go"}`)},
		},
	})
	if got.Content != "hi" || got.StopReason != StopReasonToolUse {
		t.Fatalf("got %+v", got)
	}
	if got.Meta.PromptTokens != 3 || got.Meta.CompletionTokens != 4 || got.Meta.DurationMs != 9 || got.Meta.CostUSD != 0 {
		t.Fatalf("meta %+v", got.Meta)
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].ID != "toolu_1" || string(got.ToolCalls[0].Arguments) != `{"q":"go"}` {
		t.Fatalf("calls %+v", got.ToolCalls)
	}
}
