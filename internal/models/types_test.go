package models

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestGenerateRequest_ToolChoiceOrDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		given string
		want  string
	}{
		{name: "zero value", given: "", want: ToolChoiceAuto},
		{name: "explicit auto", given: ToolChoiceAuto, want: ToolChoiceAuto},
		{name: "none", given: ToolChoiceNone, want: ToolChoiceNone},
		{name: "required", given: ToolChoiceRequired, want: ToolChoiceRequired},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := GenerateRequest{ToolChoice: tc.given}
			if got := req.ToolChoiceOrDefault(); got != tc.want {
				t.Fatalf("ToolChoiceOrDefault() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestToolDef_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	assertJSONRoundTrip(t, ToolDef{
		Name:        "get_weather",
		Description: "Return weather for a city",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	})
}

func TestToolCall_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	assertJSONRoundTrip(t, ToolCall{
		ID:        "call_abc123",
		Name:      "get_weather",
		Arguments: json.RawMessage(`{"city":"Paris"}`),
	})
}

func TestToolResult_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	assertJSONRoundTrip(t, ToolResult{
		ToolCallID: "call_abc123",
		Content:    `{"temp_c":18}`,
	})
}

func TestChatMessage_JSONRoundTrip_textOnly(t *testing.T) {
	t.Parallel()
	assertJSONRoundTrip(t, ChatMessage{Role: "user", Content: "hello"})
}

func TestChatMessage_JSONRoundTrip_withToolResults(t *testing.T) {
	t.Parallel()
	assertJSONRoundTrip(t, ChatMessage{
		Role: "user",
		ToolResults: []ToolResult{
			{ToolCallID: "call_1", Content: "ok"},
			{ToolCallID: "call_2", Content: `{"n":2}`},
		},
	})
}

func TestGenerateRequest_JSONRoundTrip_minimal(t *testing.T) {
	t.Parallel()
	// Existing two-field call sites remain valid without tools or tool choice.
	assertJSONRoundTrip(t, GenerateRequest{
		Model:    "gpt-4.1",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
}

func TestGenerateRequest_JSONRoundTrip_withTools(t *testing.T) {
	t.Parallel()
	assertJSONRoundTrip(t, GenerateRequest{
		Model: "claude-sonnet",
		Messages: []ChatMessage{
			{Role: "system", Content: "be brief"},
			{Role: "user", Content: "weather?"},
		},
		Tools: []ToolDef{
			{
				Name:        "get_weather",
				Description: "City weather",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		},
		ToolChoice: ToolChoiceRequired,
	})
}

func TestGenerateResponse_JSONRoundTrip_endTurn(t *testing.T) {
	t.Parallel()
	assertJSONRoundTrip(t, GenerateResponse{
		Content:    "done",
		StopReason: StopReasonEndTurn,
		Meta: GenerateMeta{
			DurationMs:       12,
			PromptTokens:     40,
			CompletionTokens: 8,
			CostUSD:          0.001,
		},
	})
}

func TestGenerateResponse_JSONRoundTrip_toolUse(t *testing.T) {
	t.Parallel()
	assertJSONRoundTrip(t, GenerateResponse{
		ToolCalls: []ToolCall{
			{ID: "call_x", Name: "search", Arguments: json.RawMessage(`{"q":"go"}`)},
		},
		StopReason: StopReasonToolUse,
		Meta:       GenerateMeta{DurationMs: 3},
	})
}

func TestGenerateMeta_JSONRoundTrip_zeroTokens(t *testing.T) {
	t.Parallel()
	assertJSONRoundTrip(t, GenerateMeta{DurationMs: 1, CostUSD: 0})
}

func TestJSONRoundTrip_emptyAndNilSlices(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		val  any
	}{
		{name: "nil message and tool slices", val: GenerateRequest{Model: "m", Messages: nil, Tools: nil}},
		{name: "empty meta", val: GenerateResponse{Meta: GenerateMeta{}}},
		{name: "nil tool calls", val: GenerateResponse{ToolCalls: nil, Meta: GenerateMeta{DurationMs: 1}}},
		{name: "nil tool results", val: ChatMessage{Role: "assistant", ToolResults: nil}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertJSONRoundTrip(t, tc.val)
		})
	}
}

func TestToolDef_JSONUnmarshal_malformedParameters(t *testing.T) {
	t.Parallel()

	var def ToolDef
	err := json.Unmarshal([]byte(`{"name":"x","parameters":{"type":"object",`), &def)
	if err == nil {
		t.Fatal("expected unmarshal error for truncated JSON parameters")
	}
}

func TestToolCall_JSONUnmarshal_invalidArgumentsJSON(t *testing.T) {
	t.Parallel()

	var call ToolCall
	err := json.Unmarshal([]byte(`{"id":"1","name":"n","arguments":{`), &call)
	if err == nil {
		t.Fatal("expected unmarshal error for truncated arguments")
	}
}

func TestGenerateRequest_JSONUnmarshal_unknownFieldsIgnored(t *testing.T) {
	t.Parallel()

	raw := `{"model":"m","messages":[],"future_field":true,"tool_choice":"auto"}`
	var req GenerateRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	if req.Model != "m" || req.ToolChoice != ToolChoiceAuto {
		t.Fatalf("got %+v", req)
	}
}

func TestJSON_omittedOptionalSlicesDecodeAsNil(t *testing.T) {
	t.Parallel()

	t.Run("request", func(t *testing.T) {
		t.Parallel()
		var got GenerateRequest
		if err := json.Unmarshal([]byte(`{"model":"m","messages":[]}`), &got); err != nil {
			t.Fatal(err)
		}
		if got.Tools != nil {
			t.Fatalf("tools = %#v, want nil when omitted", got.Tools)
		}
	})

	t.Run("response", func(t *testing.T) {
		t.Parallel()
		var got GenerateResponse
		if err := json.Unmarshal([]byte(`{"meta":{"duration_ms":1}}`), &got); err != nil {
			t.Fatal(err)
		}
		if got.ToolCalls != nil {
			t.Fatalf("tool_calls = %#v, want nil when omitted", got.ToolCalls)
		}
	})
}

func TestJSON_explicitEmptySlicesDecodeAsEmpty(t *testing.T) {
	t.Parallel()

	t.Run("tools", func(t *testing.T) {
		t.Parallel()
		var got GenerateRequest
		if err := json.Unmarshal([]byte(`{"model":"m","messages":[],"tools":[]}`), &got); err != nil {
			t.Fatal(err)
		}
		if got.Tools == nil || len(got.Tools) != 0 {
			t.Fatalf("tools = %#v, want non-nil empty slice", got.Tools)
		}
	})
}

func TestGenerateResponse_JSONUnmarshal_preservesUnknownStopReason(t *testing.T) {
	t.Parallel()

	raw := `{"stop_reason":"provider_specific","meta":{"duration_ms":1}}`
	var resp GenerateResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != "provider_specific" {
		t.Fatalf("stop_reason %q", resp.StopReason)
	}
}

func assertJSONRoundTrip(t *testing.T, original any) {
	t.Helper()

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	dst := reflect.New(reflect.TypeOf(original))
	if err := json.Unmarshal(encoded, dst.Interface()); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := dst.Elem().Interface()
	if !reflect.DeepEqual(original, got) {
		t.Fatalf("round-trip mismatch\noriginal: %#v\ngot:      %#v\njson: %s", original, got, encoded)
	}
}
