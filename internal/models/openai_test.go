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
)

func weatherTool() ToolDef {
	return ToolDef{
		Name:        "get_weather",
		Description: "Return weather for a city",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}
}

func TestOpenAIClient_Generate_toolCalling(t *testing.T) {
	t.Parallel()

	type captured struct {
		Model      string `json:"model"`
		ToolChoice string `json:"tool_choice"`
		Tools      []struct {
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
		Messages []struct {
			Role       string `json:"role"`
			Content    any    `json:"content"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
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
				Model:    "gpt-4o-mini",
				Messages: []ChatMessage{{Role: "user", Content: "weather in Paris?"}},
				Tools:    []ToolDef{weatherTool()},
			},
			fixture: "chat_tool_calls.json",
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if got.ToolChoice != ToolChoiceAuto {
					t.Fatalf("tool_choice %q", got.ToolChoice)
				}
				if len(got.Tools) != 1 || got.Tools[0].Type != "function" || got.Tools[0].Function.Name != "get_weather" {
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
				want := []ToolCall{{ID: "call_abc123", Name: "get_weather", Arguments: json.RawMessage(`{"city":"Paris"}`)}}
				if !reflect.DeepEqual(resp.ToolCalls, want) {
					t.Fatalf("ToolCalls %#v", resp.ToolCalls)
				}
				if resp.Meta.PromptTokens != 82 || resp.Meta.CompletionTokens != 18 {
					t.Fatalf("tokens %+v", resp.Meta)
				}
				if resp.Meta.CostUSD <= 0 {
					t.Fatalf("CostUSD %v", resp.Meta.CostUSD)
				}
			},
		},
		{
			name: "tool_use_parallel",
			req: GenerateRequest{
				Model:    "gpt-4o-mini",
				Messages: []ChatMessage{{Role: "user", Content: "weather and time"}},
				Tools:    []ToolDef{weatherTool(), {Name: "get_time", Parameters: json.RawMessage(`{"type":"object"}`)}},
			},
			fixture: "chat_multi_tool_calls.json",
			checkResp: func(t *testing.T, resp GenerateResponse) {
				t.Helper()
				if resp.StopReason != StopReasonToolUse {
					t.Fatalf("StopReason %q", resp.StopReason)
				}
				if len(resp.ToolCalls) != 2 {
					t.Fatalf("ToolCalls %#v", resp.ToolCalls)
				}
				if resp.ToolCalls[0].ID != "call_1" || resp.ToolCalls[0].Name != "get_weather" {
					t.Fatalf("first %#v", resp.ToolCalls[0])
				}
				if resp.ToolCalls[1].ID != "call_2" || resp.ToolCalls[1].Name != "get_time" {
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
				Model:    "gpt-4o-mini",
				Messages: []ChatMessage{{Role: "user", Content: "hi"}},
				Tools:    []ToolDef{weatherTool()},
			},
			fixture: "chat_end_turn.json",
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
				Model:    "gpt-4o-mini",
				Messages: []ChatMessage{{Role: "user", Content: "hi"}},
				Tools:    []ToolDef{weatherTool()},
			},
			fixture: "chat_max_tokens.json",
			checkResp: func(t *testing.T, resp GenerateResponse) {
				t.Helper()
				if resp.StopReason != StopReasonMaxTokens {
					t.Fatalf("StopReason %q", resp.StopReason)
				}
			},
		},
		{
			name: "unknown_finish",
			req: GenerateRequest{
				Model:    "gpt-4o-mini",
				Messages: []ChatMessage{{Role: "user", Content: "hi"}},
			},
			fixture: "chat_unknown_finish.json",
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if got.ToolChoice != "" || len(got.Tools) != 0 {
					t.Fatalf("unexpected tools/choice %+v", got)
				}
			},
			checkResp: func(t *testing.T, resp GenerateResponse) {
				t.Helper()
				if resp.StopReason != "content_filter" {
					t.Fatalf("StopReason %q", resp.StopReason)
				}
			},
		},
		{
			name: "tool_choice_none",
			req: GenerateRequest{
				Model:      "gpt-4o-mini",
				Messages:   []ChatMessage{{Role: "user", Content: "hi"}},
				Tools:      []ToolDef{weatherTool()},
				ToolChoice: ToolChoiceNone,
			},
			fixture: "chat_end_turn.json",
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if got.ToolChoice != ToolChoiceNone {
					t.Fatalf("tool_choice %q", got.ToolChoice)
				}
			},
		},
		{
			name: "tool_choice_required",
			req: GenerateRequest{
				Model:      "gpt-4o-mini",
				Messages:   []ChatMessage{{Role: "user", Content: "weather?"}},
				Tools:      []ToolDef{weatherTool()},
				ToolChoice: ToolChoiceRequired,
			},
			fixture: "chat_tool_calls.json",
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if got.ToolChoice != ToolChoiceRequired {
					t.Fatalf("tool_choice %q", got.ToolChoice)
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
				Model:    "gpt-4o-mini",
				Messages: []ChatMessage{{Role: "user", Content: "hi"}},
			},
			inlineBody: `{"choices":[{"message":{"content":"hello"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if got.ToolChoice != "" || len(got.Tools) != 0 {
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
				Model: "gpt-4o-mini",
				Messages: []ChatMessage{
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
				},
				Tools: []ToolDef{weatherTool()},
			},
			fixture: "chat_end_turn.json",
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if len(got.Messages) != 3 {
					t.Fatalf("messages %+v", got.Messages)
				}
				if got.Messages[1].Role != "assistant" || len(got.Messages[1].ToolCalls) != 1 {
					t.Fatalf("assistant %+v", got.Messages[1])
				}
				if got.Messages[1].ToolCalls[0].ID != "call_abc123" || got.Messages[1].ToolCalls[0].Function.Arguments != `{"city":"Paris"}` {
					t.Fatalf("tool_calls %+v", got.Messages[1].ToolCalls)
				}
				if got.Messages[1].Content != nil {
					t.Fatalf("assistant content should be null, got %#v", got.Messages[1].Content)
				}
				if got.Messages[2].Role != "tool" || got.Messages[2].ToolCallID != "call_abc123" {
					t.Fatalf("tool message %+v", got.Messages[2])
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
			name: "length_truncated_tool_call_args",
			req: GenerateRequest{
				Model:    "gpt-4o-mini",
				Messages: []ChatMessage{{Role: "user", Content: "weather?"}},
				Tools:    []ToolDef{weatherTool()},
			},
			inlineBody: `{"choices":[{"finish_reason":"length","message":{"content":null,"tool_calls":[{"id":"call_abc123","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Par"}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":4}}`,
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
			name: "stop_reason_with_tool_calls",
			req: GenerateRequest{
				Model:    "gpt-4o-mini",
				Messages: []ChatMessage{{Role: "user", Content: "weather?"}},
				Tools:    []ToolDef{weatherTool()},
			},
			inlineBody: `{"choices":[{"finish_reason":"stop","message":{"content":null,"tool_calls":[{"id":"call_abc123","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
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
				Model: "gpt-4o-mini",
				Messages: []ChatMessage{
					{
						Role: "assistant",
						ToolCalls: []ToolCall{
							{ID: "call_abc123", Name: "get_weather", Arguments: json.RawMessage(`{"city":"Paris"}`)},
						},
					},
					{
						Role:    "user",
						Content: "summarize that",
						ToolResults: []ToolResult{
							{ToolCallID: "call_abc123", Content: `{"temp_c":18}`},
						},
					},
				},
				Tools: []ToolDef{weatherTool()},
			},
			fixture: "chat_end_turn.json",
			checkReq: func(t *testing.T, got captured) {
				t.Helper()
				if len(got.Messages) != 3 {
					t.Fatalf("messages %+v", got.Messages)
				}
				if got.Messages[0].Role != "assistant" || len(got.Messages[0].ToolCalls) != 1 {
					t.Fatalf("assistant %+v", got.Messages[0])
				}
				if got.Messages[1].Role != "tool" || got.Messages[1].ToolCallID != "call_abc123" {
					t.Fatalf("tool %+v", got.Messages[1])
				}
				if got.Messages[2].Role != "user" {
					t.Fatalf("follow-up %+v", got.Messages[2])
				}
				text, _ := got.Messages[2].Content.(string)
				if text != "summarize that" {
					t.Fatalf("follow-up content %#v", got.Messages[2].Content)
				}
			},
		},
		{
			name: "empty_tool_call_id_before_http",
			req: GenerateRequest{
				Model: "gpt-4o-mini",
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
				Model: "gpt-4o-mini",
				Messages: []ChatMessage{
					{Role: "assistant", ToolCalls: []ToolCall{{Name: "get_weather"}}},
				},
				Tools: []ToolDef{weatherTool()},
			},
			noHTTP:  true,
			wantErr: "missing id",
		},
		{
			name: "invalid_arguments",
			req: GenerateRequest{
				Model:    "gpt-4o-mini",
				Messages: []ChatMessage{{Role: "user", Content: "x"}},
				Tools:    []ToolDef{weatherTool()},
			},
			fixture: "chat_invalid_arguments.json",
			wantErr: "arguments are not JSON",
		},
		{
			name: "unsupported_tool_choice",
			req: GenerateRequest{
				Model:      "gpt-4o-mini",
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
				c := &OpenAIClient{APIKey: "sk-mock", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					t.Fatal("HTTP should not be called")
					return nil, nil
				})}}
				_, err := c.Generate(context.Background(), tc.req)
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}

			body := []byte(tc.inlineBody)
			if tc.fixture != "" {
				var err error
				body, err = os.ReadFile(filepath.Join("testdata", "openai", tc.fixture))
				if err != nil {
					t.Fatal(err)
				}
			}

			var saw captured
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/chat/completions" {
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

			c := &OpenAIClient{APIKey: "sk-mock", BaseURL: srv.URL + "/v1", HTTPClient: srv.Client()}
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenAIClient_Generate_HTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()

	c := &OpenAIClient{APIKey: "bad", BaseURL: srv.URL + "/v1", HTTPClient: srv.Client()}
	_, err := c.Generate(context.Background(), GenerateRequest{
		Model:    "gpt-4o-mini",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("got %v", err)
	}
}

func TestOpenAIClient_Generate_notConfigured(t *testing.T) {
	t.Parallel()
	_, err := (*OpenAIClient)(nil).Generate(context.Background(), GenerateRequest{})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("got %v", err)
	}
}
