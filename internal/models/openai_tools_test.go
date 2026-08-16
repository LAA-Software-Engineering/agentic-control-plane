package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMapOpenAIToolChoice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		given   string
		want    string
		wantErr string
	}{
		{name: "empty is auto", given: "", want: ToolChoiceAuto},
		{name: "auto", given: ToolChoiceAuto, want: ToolChoiceAuto},
		{name: "none", given: ToolChoiceNone, want: ToolChoiceNone},
		{name: "required", given: ToolChoiceRequired, want: ToolChoiceRequired},
		{name: "unsupported", given: "force_foo", wantErr: `unsupported tool_choice "force_foo"`},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := mapOpenAIToolChoice(tc.given)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestMapOpenAIStopReason(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		finish string
		nCalls int
		want   string
	}{
		{name: "tool_calls", finish: "tool_calls", want: StopReasonToolUse},
		{name: "stop no calls", finish: "stop", want: StopReasonEndTurn},
		{name: "stop with calls", finish: "stop", nCalls: 1, want: StopReasonToolUse},
		{name: "length no calls", finish: "length", want: StopReasonMaxTokens},
		{name: "length with calls", finish: "length", nCalls: 1, want: StopReasonMaxTokens},
		{name: "empty with calls", finish: "", nCalls: 1, want: StopReasonToolUse},
		{name: "empty no calls", finish: "", nCalls: 0, want: StopReasonEndTurn},
		{name: "content_filter no calls", finish: "content_filter", want: "content_filter"},
		{name: "content_filter with calls", finish: "content_filter", nCalls: 1, want: "content_filter"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := mapOpenAIStopReason(tc.finish, tc.nCalls); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestBuildOpenAIChatPayload_toolsAndChoice(t *testing.T) {
	t.Parallel()
	body, err := buildOpenAIChatPayload(GenerateRequest{
		Model: "gpt-4o-mini",
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
	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["tool_choice"]) != `"auto"` {
		t.Fatalf("tool_choice %s", got["tool_choice"])
	}
	var tools []openaiTool
	if err := json.Unmarshal(got["tools"], &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Type != "function" || tools[0].Function.Name != "get_weather" {
		t.Fatalf("tools %+v", tools)
	}
	if string(tools[0].Function.Parameters) != `{"type":"object"}` {
		t.Fatalf("parameters %s", tools[0].Function.Parameters)
	}
}

func TestBuildOpenAIChatPayload_omitsToolsWhenEmpty(t *testing.T) {
	t.Parallel()
	body, err := buildOpenAIChatPayload(GenerateRequest{
		Model:    "gpt-4o-mini",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["tools"]; ok {
		t.Fatalf("tools present: %v", got["tools"])
	}
	if _, ok := got["tool_choice"]; ok {
		t.Fatalf("tool_choice present: %v", got["tool_choice"])
	}
}

func TestBuildOpenAIChatPayload_emptyParametersDefault(t *testing.T) {
	t.Parallel()
	body, err := buildOpenAIChatPayload(GenerateRequest{
		Model:    "m",
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
		Tools:    []ToolDef{{Name: "noop"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload openaiChatRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if string(payload.Tools[0].Function.Parameters) != `{"type":"object","properties":{}}` {
		t.Fatalf("parameters %s", payload.Tools[0].Function.Parameters)
	}
}

func TestBuildOpenAIChatPayload_rejectsNonObjectParameters(t *testing.T) {
	t.Parallel()
	_, err := buildOpenAIChatPayload(GenerateRequest{
		Model:    "m",
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
		Tools:    []ToolDef{{Name: "noop", Parameters: json.RawMessage(`[]`)}},
	})
	if err == nil || !strings.Contains(err.Error(), "must be a JSON object") {
		t.Fatalf("got %v", err)
	}
}

func TestMapOpenAIMessages_toolResultsRoundTrip(t *testing.T) {
	t.Parallel()
	msgs, err := mapOpenAIMessages([]ChatMessage{
		{Role: "user", Content: "weather in Paris?"},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "call_abc123", Name: "get_weather", Arguments: json.RawMessage(`{"city":"Paris"}`)},
			},
		},
		{
			Role: "user",
			ToolResults: []ToolResult{
				{ToolCallID: "call_abc123", Content: `{"temp_c":18}`},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len=%d msgs=%+v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || msgs[0].Content == nil || *msgs[0].Content != "weather in Paris?" {
		t.Fatalf("user %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != nil || len(msgs[1].ToolCalls) != 1 {
		t.Fatalf("assistant %+v", msgs[1])
	}
	if msgs[1].ToolCalls[0].ID != "call_abc123" || msgs[1].ToolCalls[0].Function.Arguments != `{"city":"Paris"}` {
		t.Fatalf("tool_calls %+v", msgs[1].ToolCalls)
	}
	if msgs[2].Role != "tool" || msgs[2].ToolCallID != "call_abc123" || msgs[2].Content == nil || *msgs[2].Content != `{"temp_c":18}` {
		t.Fatalf("tool %+v", msgs[2])
	}
}

func TestMapOpenAIMessages_contentAndToolResults(t *testing.T) {
	t.Parallel()
	msgs, err := mapOpenAIMessages([]ChatMessage{
		{
			Role:    "user",
			Content: "use that result",
			ToolResults: []ToolResult{
				{ToolCallID: "call_abc123", Content: `{"temp_c":18}`},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len=%d msgs=%+v", len(msgs), msgs)
	}
	if msgs[0].Role != "tool" || msgs[0].ToolCallID != "call_abc123" {
		t.Fatalf("tool first %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content == nil || *msgs[1].Content != "use that result" {
		t.Fatalf("text after tools %+v", msgs[1])
	}
}

func TestMapOpenAIMessages_rejectsNonAssistantToolCalls(t *testing.T) {
	t.Parallel()
	_, err := mapOpenAIMessages([]ChatMessage{
		{Role: "user", ToolCalls: []ToolCall{{ID: "c1", Name: "x", Arguments: json.RawMessage(`{}`)}}},
	})
	if err == nil || !strings.Contains(err.Error(), "require assistant role") {
		t.Fatalf("got %v", err)
	}
}

func TestMapOpenAIMessages_emptyFollowUpRoleDefaultsUser(t *testing.T) {
	t.Parallel()
	msgs, err := mapOpenAIMessages([]ChatMessage{
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
	if len(msgs) != 2 || msgs[1].Role != "user" {
		t.Fatalf("msgs %+v", msgs)
	}
}

func TestMapOpenAIMessages_emptyToolCallID(t *testing.T) {
	t.Parallel()
	_, err := mapOpenAIMessages([]ChatMessage{
		{ToolResults: []ToolResult{{Content: "ok"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "missing tool_call_id") {
		t.Fatalf("got %v", err)
	}
}

func TestEncodeOpenAIToolCalls_requiresIDAndName(t *testing.T) {
	t.Parallel()
	_, err := encodeOpenAIToolCalls([]ToolCall{{Name: "get_weather", Arguments: json.RawMessage(`{}`)}})
	if err == nil || !strings.Contains(err.Error(), "missing id") {
		t.Fatalf("id err %v", err)
	}
	_, err = encodeOpenAIToolCalls([]ToolCall{{ID: "call_1"}})
	if err == nil || !strings.Contains(err.Error(), "empty name") {
		t.Fatalf("name err %v", err)
	}
}

func TestEncodeOpenAIToolCalls_trimsIDAndName(t *testing.T) {
	t.Parallel()
	got, err := encodeOpenAIToolCalls([]ToolCall{
		{ID: "  call_1  ", Name: "  get_weather  ", Arguments: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ID != "call_1" || got[0].Function.Name != "get_weather" {
		t.Fatalf("got %+v", got[0])
	}
}

func TestParseOpenAIChatResponse_toolCalls(t *testing.T) {
	t.Parallel()
	body := []byte(`{"choices":[{"finish_reason":"tool_calls","message":{"content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"search","arguments":"{\"q\":\"go\"}"}}]}}],"usage":{"prompt_tokens":2,"completion_tokens":3}}`)
	content, calls, stop, pt, ct, err := parseOpenAIChatResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if content != "" || stop != StopReasonToolUse || pt != 2 || ct != 3 {
		t.Fatalf("content=%q stop=%q pt=%d ct=%d", content, stop, pt, ct)
	}
	if len(calls) != 1 || calls[0].ID != "c1" || calls[0].Name != "search" || string(calls[0].Arguments) != `{"q":"go"}` {
		t.Fatalf("calls %+v", calls)
	}
}

func TestParseOpenAIChatResponse_stopWithToolCalls(t *testing.T) {
	t.Parallel()
	body := []byte(`{"choices":[{"finish_reason":"stop","message":{"content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"search","arguments":"{\"q\":\"go\"}"}}]}}]}`)
	_, calls, stop, _, _, err := parseOpenAIChatResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if stop != StopReasonToolUse || len(calls) != 1 {
		t.Fatalf("stop=%q calls=%+v", stop, calls)
	}
}

func TestParseOpenAIChatResponse_emptyArguments(t *testing.T) {
	t.Parallel()
	body := []byte(`{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"id":"c1","function":{"name":"noop","arguments":""}}]}}]}`)
	_, calls, stop, _, _, err := parseOpenAIChatResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if stop != StopReasonToolUse || len(calls) != 1 || string(calls[0].Arguments) != "{}" {
		t.Fatalf("stop=%q calls=%+v", stop, calls)
	}
}

func TestParseOpenAIChatResponse_lengthWithTruncatedArguments(t *testing.T) {
	t.Parallel()
	body := []byte(`{"choices":[{"finish_reason":"length","message":{"content":null,"tool_calls":[{"id":"c1","function":{"name":"search","arguments":"{\"q\":\"go"}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":4}}`)
	content, calls, stop, pt, ct, err := parseOpenAIChatResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if stop != StopReasonMaxTokens {
		t.Fatalf("stop %q", stop)
	}
	if len(calls) != 0 {
		t.Fatalf("calls %+v, want none on truncated args", calls)
	}
	if content != "" || pt != 10 || ct != 4 {
		t.Fatalf("content=%q pt=%d ct=%d", content, pt, ct)
	}
}

func TestParseOpenAIChatResponse_contentFilterWithToolCalls(t *testing.T) {
	t.Parallel()
	body := []byte(`{"choices":[{"finish_reason":"content_filter","message":{"tool_calls":[{"id":"c1","function":{"name":"search","arguments":"{}"}}]}}]}`)
	_, calls, stop, _, _, err := parseOpenAIChatResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if stop != "content_filter" {
		t.Fatalf("stop %q", stop)
	}
	if len(calls) != 0 {
		t.Fatalf("calls %+v, want none when StopReason is not tool_use", calls)
	}
}

func TestParseOpenAIChatResponse_lengthWithCompleteToolCalls(t *testing.T) {
	t.Parallel()
	body := []byte(`{"choices":[{"finish_reason":"length","message":{"tool_calls":[{"id":"c1","function":{"name":"search","arguments":"{\"q\":\"go\"}"}}]}}]}`)
	_, calls, stop, _, _, err := parseOpenAIChatResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if stop != StopReasonMaxTokens {
		t.Fatalf("stop %q", stop)
	}
	if len(calls) != 0 {
		t.Fatalf("calls %+v, want none on length", calls)
	}
}

func TestOpenAITextRole_trims(t *testing.T) {
	t.Parallel()
	if got := openaiTextRole("  user  "); got != "user" {
		t.Fatalf("got %q", got)
	}
	if got := openaiTextRole("  "); got != "user" {
		t.Fatalf("whitespace got %q", got)
	}
	if got := openaiTextRole("assistant"); got != "assistant" {
		t.Fatalf("assistant got %q", got)
	}
}

func TestParseOpenAIChatResponse_invalidArguments(t *testing.T) {
	t.Parallel()
	_, _, _, _, _, err := parseOpenAIChatResponse([]byte(`{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"id":"call_bad","function":{"name":"x","arguments":"not-json"}}]}}]}`))
	if err == nil || !strings.Contains(err.Error(), "arguments are not JSON") {
		t.Fatalf("got %v", err)
	}
}

func TestParseOpenAIChatResponse_emptyFunctionName(t *testing.T) {
	t.Parallel()
	_, _, _, _, _, err := parseOpenAIChatResponse([]byte(`{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"id":"call_ok","function":{"name":"search","arguments":"{}"}},{"id":"call_bad","function":{"name":"","arguments":"{}"}}]}}]}`))
	if err == nil || !strings.Contains(err.Error(), `tool call "call_bad": empty function name`) {
		t.Fatalf("got %v", err)
	}
}

func TestParseOpenAIChatResponse_toolCallsFinishWithoutCalls(t *testing.T) {
	t.Parallel()
	_, _, _, _, _, err := parseOpenAIChatResponse([]byte(`{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[]}}]}`))
	if err == nil || !strings.Contains(err.Error(), "tool_calls finish without calls") {
		t.Fatalf("got %v", err)
	}
}
