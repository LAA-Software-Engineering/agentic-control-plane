package anthropic

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMapStopReason(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		stop   string
		nCalls int
		want   string
	}{
		{name: "tool_use", stop: stopToolUse, want: stopToolUse},
		{name: "end_turn no calls", stop: stopEndTurn, want: stopEndTurn},
		{name: "end_turn with calls", stop: stopEndTurn, nCalls: 1, want: stopToolUse},
		{name: "max_tokens no calls", stop: stopMaxTokens, want: stopMaxTokens},
		{name: "max_tokens with calls", stop: stopMaxTokens, nCalls: 1, want: stopMaxTokens},
		{name: "empty with calls", stop: "", nCalls: 1, want: stopToolUse},
		{name: "empty no calls", stop: "", nCalls: 0, want: stopEndTurn},
		{name: "refusal no calls", stop: stopRefusal, want: stopRefusal},
		{name: "refusal with calls", stop: stopRefusal, nCalls: 1, want: stopRefusal},
		{name: "stop_sequence", stop: "stop_sequence", want: "stop_sequence"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := mapStopReason(tc.stop, tc.nCalls); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestParseResponse_toolUse(t *testing.T) {
	t.Parallel()
	body := []byte(`{"content":[{"type":"tool_use","id":"toolu_1","name":"search","input":{"q":"go"}}],"stop_reason":"tool_use","usage":{"input_tokens":2,"output_tokens":3}}`)
	got, err := parseResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "" || got.StopReason != stopToolUse || got.InputTokens != 2 || got.OutputTokens != 3 {
		t.Fatalf("got %+v", got)
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].ID != "toolu_1" || got.ToolCalls[0].Name != "search" {
		t.Fatalf("calls %+v", got.ToolCalls)
	}
	if string(got.ToolCalls[0].Input) != `{"q":"go"}` {
		t.Fatalf("input %s", got.ToolCalls[0].Input)
	}
}

func TestParseResponse_endTurnWithToolUse(t *testing.T) {
	t.Parallel()
	body := []byte(`{"content":[{"type":"tool_use","id":"toolu_1","name":"search","input":{"q":"go"}}],"stop_reason":"end_turn"}`)
	got, err := parseResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.StopReason != stopToolUse || len(got.ToolCalls) != 1 {
		t.Fatalf("stop=%q calls=%+v", got.StopReason, got.ToolCalls)
	}
}

func TestParseResponse_emptyInput(t *testing.T) {
	t.Parallel()
	body := []byte(`{"content":[{"type":"tool_use","id":"toolu_1","name":"noop"}],"stop_reason":"tool_use"}`)
	got, err := parseResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.StopReason != stopToolUse || len(got.ToolCalls) != 1 || string(got.ToolCalls[0].Input) != "{}" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseResponse_maxTokensWithTruncatedInput(t *testing.T) {
	t.Parallel()
	body := []byte(`{"content":[{"type":"tool_use","id":"toolu_1","name":"search","input":"not-object"}],"stop_reason":"max_tokens","usage":{"input_tokens":10,"output_tokens":4}}`)
	got, err := parseResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.StopReason != stopMaxTokens {
		t.Fatalf("stop %q", got.StopReason)
	}
	if len(got.ToolCalls) != 0 {
		t.Fatalf("calls %+v, want none on truncated input", got.ToolCalls)
	}
	if got.InputTokens != 10 || got.OutputTokens != 4 {
		t.Fatalf("tokens %+v", got)
	}
}

func TestParseResponse_refusalWithToolUse(t *testing.T) {
	t.Parallel()
	body := []byte(`{"content":[{"type":"tool_use","id":"toolu_1","name":"search","input":{}}],"stop_reason":"refusal"}`)
	got, err := parseResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.StopReason != stopRefusal {
		t.Fatalf("stop %q", got.StopReason)
	}
	if len(got.ToolCalls) != 0 {
		t.Fatalf("calls %+v, want none when StopReason is not tool_use", got.ToolCalls)
	}
}

func TestParseResponse_maxTokensWithCompleteToolUse(t *testing.T) {
	t.Parallel()
	body := []byte(`{"content":[{"type":"tool_use","id":"toolu_1","name":"search","input":{"q":"go"}}],"stop_reason":"max_tokens"}`)
	got, err := parseResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.StopReason != stopMaxTokens {
		t.Fatalf("stop %q", got.StopReason)
	}
	if len(got.ToolCalls) != 0 {
		t.Fatalf("calls %+v, want none on max_tokens", got.ToolCalls)
	}
}

func TestParseResponse_invalidInput(t *testing.T) {
	t.Parallel()
	_, err := parseResponse([]byte(`{"content":[{"type":"tool_use","id":"toolu_bad","name":"x","input":"not-object"}],"stop_reason":"tool_use"}`))
	if err == nil || !strings.Contains(err.Error(), "input must be a JSON object") {
		t.Fatalf("got %v", err)
	}
}

func TestParseResponse_emptyName(t *testing.T) {
	t.Parallel()
	_, err := parseResponse([]byte(`{"content":[{"type":"tool_use","id":"toolu_ok","name":"search","input":{}},{"type":"tool_use","id":"toolu_bad","name":"","input":{}}],"stop_reason":"tool_use"}`))
	if err == nil || !strings.Contains(err.Error(), `tool_use "toolu_bad": empty name`) {
		t.Fatalf("got %v", err)
	}
}

func TestParseResponse_endTurnNoText(t *testing.T) {
	t.Parallel()
	_, err := parseResponse([]byte(`{"content":[],"stop_reason":"end_turn"}`))
	if err == nil || !strings.Contains(err.Error(), "no text content in response") {
		t.Fatalf("got %v", err)
	}
	_, err = parseResponse([]byte(`{"content":[{"type":"thinking","thinking":"hmm"}]}`))
	if err == nil || !strings.Contains(err.Error(), "no text content in response") {
		t.Fatalf("empty stop_reason got %v", err)
	}
}

func TestParseResponse_toolUseStopWithoutBlocks(t *testing.T) {
	t.Parallel()
	_, err := parseResponse([]byte(`{"content":[],"stop_reason":"tool_use"}`))
	if err == nil || !strings.Contains(err.Error(), "tool_use stop without tool_use blocks") {
		t.Fatalf("got %v", err)
	}
}

func TestParseResponse_concatTextAndIgnoreThinking(t *testing.T) {
	t.Parallel()
	body := []byte(`{"content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"a"},{"type":"text","text":"b"}],"stop_reason":"end_turn"}`)
	got, err := parseResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "ab" || got.StopReason != stopEndTurn {
		t.Fatalf("got %+v", got)
	}
}

func TestNormalizeInputObject_compactsPrettyJSON(t *testing.T) {
	t.Parallel()
	got, err := normalizeInputObject(json.RawMessage("{\n  \"city\": \"Paris\"\n}"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"city":"Paris"}` {
		t.Fatalf("got %s", got)
	}
}

func TestContentBlock_MarshalJSON_emptyToolResultContent(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(ContentBlock{Type: "tool_result", ToolUseID: "toolu_1", Content: ""})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"content":""`) {
		t.Fatalf("empty tool_result omitted content: %s", raw)
	}
	if !strings.Contains(string(raw), `"tool_use_id":"toolu_1"`) {
		t.Fatalf("tool_use_id missing: %s", raw)
	}
	text, err := json.Marshal(ContentBlock{Type: "text", Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), `"content"`) {
		t.Fatalf("text block should omit content: %s", text)
	}
}

func TestChatMessage_MarshalJSON_stringVsBlocks(t *testing.T) {
	t.Parallel()
	plain, err := json.Marshal(ChatMessage{Role: "user", Content: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != `{"role":"user","content":"hi"}` {
		t.Fatalf("plain %s", plain)
	}
	blocks, err := json.Marshal(ChatMessage{
		Role: "assistant",
		Blocks: []ContentBlock{
			{Type: "tool_use", ID: "toolu_1", Name: "noop", Input: json.RawMessage(`{}`)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(blocks, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["role"]) != `"assistant"` {
		t.Fatalf("role %s", got["role"])
	}
	var arr []ContentBlock
	if err := json.Unmarshal(got["content"], &arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 1 || arr[0].Type != "tool_use" || arr[0].ID != "toolu_1" {
		t.Fatalf("blocks %+v", arr)
	}
}
