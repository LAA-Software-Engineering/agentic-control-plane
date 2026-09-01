package models

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/models/anthropic"
)

func TestAnthropicClient_Generate_toolCalling(t *testing.T) {
	t.Parallel()

	type captured struct {
		Model      string `json:"model"`
		System     string `json:"system"`
		ToolChoice *struct {
			Type string `json:"type"`
		} `json:"tool_choice"`
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"input_schema"`
		} `json:"tools"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}

	tests := []struct {
		name       string
		req        GenerateRequest
		fixture    string
		inlineBody string
		wantErr    string
		noHTTP     bool
		checkReq   func(*testing.T, captured)
		checkResp  func(*testing.T, GenerateResponse)
	}{
		{
			name: "tool_use_single",
			req: GenerateRequest{
				Model:    "claude-sonnet-4-20250514",
				Messages: []ChatMessage{{Role: "user", Content: "weather in Paris?"}},
				Tools:    []ToolDef{weatherTool()},
			},
			fixture: "messages_tool_use.json",
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if got.ToolChoice == nil || got.ToolChoice.Type != "auto" {
					t.Fatalf("tool_choice %+v", got.ToolChoice)
				}
				if len(got.Tools) != 1 || got.Tools[0].Name != "get_weather" {
					t.Fatalf("tools %+v", got.Tools)
				}
			},
			checkResp: func(t *testing.T, resp GenerateResponse) {
				t.Helper()
				if resp.StopReason != StopReasonToolUse {
					t.Fatalf("StopReason %q", resp.StopReason)
				}
				if resp.Content != "" {
					t.Fatalf("Content %q", resp.Content)
				}
				want := []ToolCall{{ID: "toolu_abc123", Name: "get_weather", Arguments: json.RawMessage(`{"city":"Paris"}`)}}
				if !reflect.DeepEqual(resp.ToolCalls, want) {
					t.Fatalf("ToolCalls %#v", resp.ToolCalls)
				}
				if resp.Meta.PromptTokens != 82 || resp.Meta.CompletionTokens != 18 {
					t.Fatalf("tokens %+v", resp.Meta)
				}
				wantCost := estimateAnthropicCostUSD("claude-sonnet-4-20250514", 82, 18)
				if resp.Meta.CostUSD != wantCost || wantCost <= 0 {
					t.Fatalf("CostUSD %v want %v", resp.Meta.CostUSD, wantCost)
				}
			},
		},
		{
			name: "tool_use_parallel",
			req: GenerateRequest{
				Model:    "claude-sonnet-4-20250514",
				Messages: []ChatMessage{{Role: "user", Content: "weather and time"}},
				Tools:    []ToolDef{weatherTool(), {Name: "get_time", Parameters: json.RawMessage(`{"type":"object"}`)}},
			},
			fixture: "messages_multi_tool_use.json",
			checkResp: func(t *testing.T, resp GenerateResponse) {
				t.Helper()
				if resp.StopReason != StopReasonToolUse {
					t.Fatalf("StopReason %q", resp.StopReason)
				}
				if len(resp.ToolCalls) != 2 {
					t.Fatalf("ToolCalls %#v", resp.ToolCalls)
				}
				if resp.ToolCalls[0].ID != "toolu_1" || resp.ToolCalls[0].Name != "get_weather" {
					t.Fatalf("first %#v", resp.ToolCalls[0])
				}
				if resp.ToolCalls[1].ID != "toolu_2" || resp.ToolCalls[1].Name != "get_time" {
					t.Fatalf("second %#v", resp.ToolCalls[1])
				}
				if string(resp.ToolCalls[1].Arguments) != `{"tz":"Europe/Paris"}` {
					t.Fatalf("args %s", resp.ToolCalls[1].Arguments)
				}
			},
		},
		{
			name: "end_turn",
			req: GenerateRequest{
				Model:    "claude-sonnet-4-20250514",
				Messages: []ChatMessage{{Role: "user", Content: "hi"}},
				Tools:    []ToolDef{weatherTool()},
			},
			fixture: "messages_end_turn.json",
			checkResp: func(t *testing.T, resp GenerateResponse) {
				t.Helper()
				if resp.StopReason != StopReasonEndTurn {
					t.Fatalf("StopReason %q", resp.StopReason)
				}
				if resp.Content != "18C and clear in Paris." {
					t.Fatalf("Content %q", resp.Content)
				}
				if len(resp.ToolCalls) != 0 {
					t.Fatalf("ToolCalls %#v", resp.ToolCalls)
				}
			},
		},
		{
			name: "max_tokens",
			req: GenerateRequest{
				Model:    "claude-sonnet-4-20250514",
				Messages: []ChatMessage{{Role: "user", Content: "hi"}},
				Tools:    []ToolDef{weatherTool()},
			},
			fixture: "messages_max_tokens.json",
			checkResp: func(t *testing.T, resp GenerateResponse) {
				t.Helper()
				if resp.StopReason != StopReasonMaxTokens {
					t.Fatalf("StopReason %q", resp.StopReason)
				}
			},
		},
		{
			name: "unknown_stop",
			req: GenerateRequest{
				Model:    "claude-sonnet-4-20250514",
				Messages: []ChatMessage{{Role: "user", Content: "hi"}},
			},
			fixture: "messages_unknown_stop.json",
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if got.ToolChoice != nil || len(got.Tools) != 0 {
					t.Fatalf("unexpected tools/choice %+v", got)
				}
			},
			checkResp: func(t *testing.T, resp GenerateResponse) {
				t.Helper()
				if resp.StopReason != "refusal" {
					t.Fatalf("StopReason %q", resp.StopReason)
				}
			},
		},
		{
			name: "tool_choice_none",
			req: GenerateRequest{
				Model:      "claude-sonnet-4-20250514",
				Messages:   []ChatMessage{{Role: "user", Content: "hi"}},
				Tools:      []ToolDef{weatherTool()},
				ToolChoice: ToolChoiceNone,
			},
			fixture: "messages_end_turn.json",
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if got.ToolChoice == nil || got.ToolChoice.Type != "none" {
					t.Fatalf("tool_choice %+v", got.ToolChoice)
				}
			},
		},
		{
			name: "tool_choice_required",
			req: GenerateRequest{
				Model:      "claude-sonnet-4-20250514",
				Messages:   []ChatMessage{{Role: "user", Content: "weather?"}},
				Tools:      []ToolDef{weatherTool()},
				ToolChoice: ToolChoiceRequired,
			},
			fixture: "messages_tool_use.json",
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if got.ToolChoice == nil || got.ToolChoice.Type != "any" {
					t.Fatalf("tool_choice %+v", got.ToolChoice)
				}
			},
			checkResp: func(t *testing.T, resp GenerateResponse) {
				t.Helper()
				if resp.StopReason != StopReasonToolUse || len(resp.ToolCalls) != 1 {
					t.Fatalf("resp %+v", resp)
				}
			},
		},
		{
			name: "tools_omitted_when_empty",
			req: GenerateRequest{
				Model:    "claude-sonnet-4-20250514",
				Messages: []ChatMessage{{Role: "user", Content: "hi"}},
			},
			inlineBody: `{"content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":1,"output_tokens":1}}`,
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if got.ToolChoice != nil || len(got.Tools) != 0 {
					t.Fatalf("tools/choice should be omitted: %+v", got)
				}
			},
			checkResp: func(t *testing.T, resp GenerateResponse) {
				t.Helper()
				if resp.Content != "hello" || resp.StopReason != StopReasonEndTurn {
					t.Fatalf("resp %+v", resp)
				}
			},
		},
		{
			name: "tool_results_round_trip",
			req: GenerateRequest{
				Model: "claude-sonnet-4-20250514",
				Messages: []ChatMessage{
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
				},
				Tools: []ToolDef{weatherTool()},
			},
			fixture: "messages_end_turn.json",
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if len(got.Messages) != 3 {
					t.Fatalf("messages %+v", got.Messages)
				}
				if got.Messages[1].Role != "assistant" {
					t.Fatalf("assistant %+v", got.Messages[1])
				}
				blocks := decodeAnthropicContentBlocks(t, got.Messages[1].Content)
				if len(blocks) != 1 || blocks[0].Type != "tool_use" || blocks[0].ID != "toolu_abc123" {
					t.Fatalf("tool_use %+v", blocks)
				}
				if string(blocks[0].Input) != `{"city":"Paris"}` {
					t.Fatalf("input %s", blocks[0].Input)
				}
				if got.Messages[2].Role != "user" {
					t.Fatalf("tool result role %+v", got.Messages[2])
				}
				results := decodeAnthropicContentBlocks(t, got.Messages[2].Content)
				if len(results) != 1 || results[0].Type != "tool_result" || results[0].ToolUseID != "toolu_abc123" {
					t.Fatalf("tool_result %+v", results)
				}
				if results[0].Content != `{"temp_c":18}` {
					t.Fatalf("result content %q", results[0].Content)
				}
			},
			checkResp: func(t *testing.T, resp GenerateResponse) {
				t.Helper()
				if resp.StopReason != StopReasonEndTurn {
					t.Fatalf("StopReason %q", resp.StopReason)
				}
			},
		},
		{
			name: "max_tokens_invalid_tool_input",
			req: GenerateRequest{
				Model:    "claude-sonnet-4-20250514",
				Messages: []ChatMessage{{Role: "user", Content: "weather?"}},
				Tools:    []ToolDef{weatherTool()},
			},
			inlineBody: `{"content":[{"type":"tool_use","id":"toolu_abc123","name":"get_weather","input":"not-object"}],"stop_reason":"max_tokens","usage":{"input_tokens":10,"output_tokens":4}}`,
			checkResp: func(t *testing.T, resp GenerateResponse) {
				t.Helper()
				if resp.StopReason != StopReasonMaxTokens {
					t.Fatalf("StopReason %q", resp.StopReason)
				}
				if len(resp.ToolCalls) != 0 {
					t.Fatalf("ToolCalls %#v, want none", resp.ToolCalls)
				}
			},
		},
		{
			name: "end_turn_with_tool_use",
			req: GenerateRequest{
				Model:    "claude-sonnet-4-20250514",
				Messages: []ChatMessage{{Role: "user", Content: "weather?"}},
				Tools:    []ToolDef{weatherTool()},
			},
			inlineBody: `{"content":[{"type":"tool_use","id":"toolu_abc123","name":"get_weather","input":{"city":"Paris"}}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`,
			checkResp: func(t *testing.T, resp GenerateResponse) {
				t.Helper()
				if resp.StopReason != StopReasonToolUse {
					t.Fatalf("StopReason %q", resp.StopReason)
				}
				if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "get_weather" {
					t.Fatalf("ToolCalls %#v", resp.ToolCalls)
				}
			},
		},
		{
			name: "content_and_tool_results",
			req: GenerateRequest{
				Model: "claude-sonnet-4-20250514",
				Messages: []ChatMessage{
					{
						Role: "assistant",
						ToolCalls: []ToolCall{
							{ID: "toolu_abc123", Name: "get_weather", Arguments: json.RawMessage(`{"city":"Paris"}`)},
						},
					},
					{
						Role:    "user",
						Content: "summarize that",
						ToolResults: []ToolResult{
							{ToolCallID: "toolu_abc123", Content: `{"temp_c":18}`},
						},
					},
				},
				Tools: []ToolDef{weatherTool()},
			},
			fixture: "messages_end_turn.json",
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if len(got.Messages) != 2 {
					t.Fatalf("messages %+v", got.Messages)
				}
				if got.Messages[0].Role != "assistant" {
					t.Fatalf("assistant %+v", got.Messages[0])
				}
				if got.Messages[1].Role != "user" {
					t.Fatalf("user %+v", got.Messages[1])
				}
				blocks := decodeAnthropicContentBlocks(t, got.Messages[1].Content)
				if len(blocks) != 2 {
					t.Fatalf("user blocks %+v", blocks)
				}
				if blocks[0].Type != "tool_result" || blocks[0].ToolUseID != "toolu_abc123" {
					t.Fatalf("tool_result %+v", blocks[0])
				}
				if blocks[1].Type != "text" || blocks[1].Text != "summarize that" {
					t.Fatalf("follow-up %+v", blocks[1])
				}
			},
		},
		{
			name: "empty_end_turn_text",
			req: GenerateRequest{
				Model:    "claude-sonnet-4-20250514",
				Messages: []ChatMessage{{Role: "user", Content: "hi"}},
			},
			inlineBody: `{"content":[],"stop_reason":"end_turn"}`,
			wantErr:    "no text content in response",
		},
		{
			name: "empty_tool_result_content",
			req: GenerateRequest{
				Model: "claude-sonnet-4-20250514",
				Messages: []ChatMessage{
					{Role: "assistant", ToolCalls: []ToolCall{{ID: "toolu_1", Name: "noop", Arguments: json.RawMessage(`{}`)}}},
					{Role: "user", ToolResults: []ToolResult{{ToolCallID: "toolu_1", Content: ""}}},
				},
				Tools: []ToolDef{{Name: "noop"}},
			},
			fixture: "messages_end_turn.json",
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if len(got.Messages) != 2 {
					t.Fatalf("messages %+v", got.Messages)
				}
				raw := string(got.Messages[1].Content)
				if !strings.Contains(raw, `"content":""`) {
					t.Fatalf("empty tool_result omitted content: %s", raw)
				}
			},
		},
		{
			name: "consecutive_tool_results_merged",
			req: GenerateRequest{
				Model: "claude-sonnet-4-20250514",
				Messages: []ChatMessage{
					{Role: "user", Content: "weather and time?"},
					{
						Role: "assistant",
						ToolCalls: []ToolCall{
							{ID: "toolu_1", Name: "get_weather", Arguments: json.RawMessage(`{"city":"Paris"}`)},
							{ID: "toolu_2", Name: "get_time", Arguments: json.RawMessage(`{"tz":"Europe/Paris"}`)},
						},
					},
					{Role: "user", ToolResults: []ToolResult{{ToolCallID: "toolu_1", Content: `{"temp_c":18}`}}},
					{Role: "user", ToolResults: []ToolResult{{ToolCallID: "toolu_2", Content: ""}}},
				},
				Tools: []ToolDef{weatherTool(), {Name: "get_time"}},
			},
			fixture: "messages_end_turn.json",
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if len(got.Messages) != 3 {
					t.Fatalf("messages %+v", got.Messages)
				}
				if got.Messages[2].Role != "user" {
					t.Fatalf("merged role %+v", got.Messages[2])
				}
				blocks := decodeAnthropicContentBlocks(t, got.Messages[2].Content)
				if len(blocks) != 2 {
					t.Fatalf("merged blocks %+v", blocks)
				}
				if blocks[0].ToolUseID != "toolu_1" || blocks[0].Content != `{"temp_c":18}` {
					t.Fatalf("first %+v", blocks[0])
				}
				if blocks[1].ToolUseID != "toolu_2" || blocks[1].Content != "" {
					t.Fatalf("second %+v", blocks[1])
				}
				if !strings.Contains(string(got.Messages[2].Content), `"content":""`) {
					t.Fatalf("empty result omitted: %s", got.Messages[2].Content)
				}
			},
		},
		{
			name: "empty_tool_call_id_before_http",
			req: GenerateRequest{
				Model: "claude-sonnet-4-20250514",
				Messages: []ChatMessage{
					{ToolResults: []ToolResult{{Content: "ok"}}},
				},
				Tools: []ToolDef{weatherTool()},
			},
			noHTTP:  true,
			wantErr: "missing tool_call_id",
		},
		{
			name: "empty_replay_id_before_http",
			req: GenerateRequest{
				Model: "claude-sonnet-4-20250514",
				Messages: []ChatMessage{
					{Role: "assistant", ToolCalls: []ToolCall{{Name: "get_weather"}}},
				},
				Tools: []ToolDef{weatherTool()},
			},
			noHTTP:  true,
			wantErr: "missing id",
		},
		{
			name: "invalid_input",
			req: GenerateRequest{
				Model:    "claude-sonnet-4-20250514",
				Messages: []ChatMessage{{Role: "user", Content: "x"}},
				Tools:    []ToolDef{weatherTool()},
			},
			fixture: "messages_invalid_input.json",
			wantErr: "input must be a JSON object",
		},
		{
			name: "unsupported_tool_choice",
			req: GenerateRequest{
				Model:      "claude-sonnet-4-20250514",
				Messages:   []ChatMessage{{Role: "user", Content: "x"}},
				Tools:      []ToolDef{weatherTool()},
				ToolChoice: "force_foo",
			},
			noHTTP:  true,
			wantErr: `unsupported tool_choice "force_foo"`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.noHTTP {
				c := &anthropicClient{inner: &anthropic.Client{APIKey: "sk-ant-mock", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					t.Fatal("HTTP should not be called")
					return nil, nil
				})}}}
				_, err := c.Generate(context.Background(), tc.req)
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}

			body := []byte(tc.inlineBody)
			if tc.fixture != "" {
				var err error
				body, err = os.ReadFile(filepath.Join("testdata", "anthropic", tc.fixture))
				if err != nil {
					t.Fatal(err)
				}
			}

			var saw captured
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/messages" {
					t.Errorf("path %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					http.Error(w, err.Error(), 500)
					return
				}
				if err := json.Unmarshal(raw, &saw); err != nil {
					t.Error(err)
					http.Error(w, err.Error(), 500)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(body)
			}))
			defer srv.Close()

			c := &anthropicClient{inner: &anthropic.Client{APIKey: "sk-ant-mock", BaseURL: srv.URL, HTTPClient: srv.Client()}}
			resp, err := c.Generate(context.Background(), tc.req)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.checkReq != nil {
				tc.checkReq(t, saw)
			}
			if tc.checkResp != nil {
				tc.checkResp(t, resp)
			}
		})
	}
}

func decodeAnthropicContentBlocks(t *testing.T, raw json.RawMessage) []anthropic.ContentBlock {
	t.Helper()
	var blocks []anthropic.ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		t.Fatalf("content %s: %v", raw, err)
	}
	return blocks
}

func TestAnthropicClient_Generate_notConfigured(t *testing.T) {
	t.Parallel()
	_, err := (*anthropicClient)(nil).Generate(context.Background(), GenerateRequest{})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("got %v", err)
	}
}
