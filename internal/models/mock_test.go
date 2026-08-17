package models

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestMockClient_scriptedTwoIterationToolLoop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tools := []ToolDef{{
		Name:        "search",
		Description: "Look things up",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`),
	}}
	cli := &MockClient{
		Script: []MockTurn{
			{
				ToolCalls: []ToolCall{{
					ID:        "call_1",
					Name:      "search",
					Arguments: json.RawMessage(`{"q":"go"}`),
				}},
				Meta: &GenerateMeta{DurationMs: 2, PromptTokens: 10, CompletionTokens: 8, CostUSD: 0.001},
			},
			{
				Content: `{"ok":true,"source":"search"}`,
				Meta:    &GenerateMeta{DurationMs: 3, PromptTokens: 24, CompletionTokens: 6, CostUSD: 0.002},
			},
		},
	}

	first, err := cli.Generate(ctx, GenerateRequest{
		Model:      "mock/loop",
		Messages:   []ChatMessage{{Role: "user", Content: "find go"}},
		Tools:      tools,
		ToolChoice: ToolChoiceAuto,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.StopReason != StopReasonToolUse {
		t.Fatalf("turn 1 StopReason %q, want %q", first.StopReason, StopReasonToolUse)
	}
	if first.Content != "" {
		t.Fatalf("turn 1 Content %q, want empty", first.Content)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].ID != "call_1" || first.ToolCalls[0].Name != "search" {
		t.Fatalf("turn 1 ToolCalls %+v", first.ToolCalls)
	}
	if string(first.ToolCalls[0].Arguments) != `{"q":"go"}` {
		t.Fatalf("turn 1 arguments %s", first.ToolCalls[0].Arguments)
	}
	if first.Meta.PromptTokens != 10 || first.Meta.CompletionTokens != 8 || first.Meta.CostUSD != 0.001 {
		t.Fatalf("turn 1 meta %+v", first.Meta)
	}
	if !reflect.DeepEqual(cli.LastRequest().Tools, tools) {
		t.Fatalf("echoed Tools %+v, want %+v", cli.LastRequest().Tools, tools)
	}

	second, err := cli.Generate(ctx, GenerateRequest{
		Model: "mock/loop",
		Messages: []ChatMessage{
			{Role: "user", Content: "find go"},
			{Role: "assistant", ToolCalls: first.ToolCalls},
			{Role: "user", ToolResults: []ToolResult{{ToolCallID: "call_1", Content: "golang.org"}}},
		},
		Tools:      tools,
		ToolChoice: ToolChoiceAuto,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.StopReason != StopReasonEndTurn {
		t.Fatalf("turn 2 StopReason %q, want %q", second.StopReason, StopReasonEndTurn)
	}
	if second.Content != `{"ok":true,"source":"search"}` {
		t.Fatalf("turn 2 Content %q", second.Content)
	}
	if len(second.ToolCalls) != 0 {
		t.Fatalf("turn 2 ToolCalls %+v, want none", second.ToolCalls)
	}
	if second.Meta.PromptTokens != 24 || second.Meta.CompletionTokens != 6 {
		t.Fatalf("turn 2 meta %+v", second.Meta)
	}

	reqs := cli.Requests()
	if len(reqs) != 2 {
		t.Fatalf("recorded %d requests, want 2", len(reqs))
	}
	if !reflect.DeepEqual(reqs[0].Tools, tools) || !reflect.DeepEqual(reqs[1].Tools, tools) {
		t.Fatalf("Tools not echoed on both turns: %+v", reqs)
	}
	if reqs[0].ToolChoice != ToolChoiceAuto {
		t.Fatalf("turn 1 ToolChoice %q", reqs[0].ToolChoice)
	}
	if len(reqs[1].Messages) != 3 || len(reqs[1].Messages[2].ToolResults) != 1 {
		t.Fatalf("turn 2 messages %+v", reqs[1].Messages)
	}
	if reqs[1].Messages[2].ToolResults[0].Content != "golang.org" {
		t.Fatalf("tool result %q", reqs[1].Messages[2].ToolResults[0].Content)
	}
	if cli.CallCount() != 2 {
		t.Fatalf("CallCount %d", cli.CallCount())
	}

	var decoded struct {
		OK     bool   `json:"ok"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal([]byte(second.Content), &decoded); err != nil {
		t.Fatalf("decode final output: %v", err)
	}
	if !decoded.OK || decoded.Source != "search" {
		t.Fatalf("decoded %+v", decoded)
	}

	_, err = cli.Generate(ctx, GenerateRequest{Model: "mock/loop"})
	if err == nil || !strings.Contains(err.Error(), "script exhausted") {
		t.Fatalf("exhausted script error = %v", err)
	}
}

func TestMockClient_legacyContentSetsEndTurn(t *testing.T) {
	t.Parallel()
	cli := &MockClient{
		Content: `{"summary":"done"}`,
		Meta:    &GenerateMeta{DurationMs: 7, PromptTokens: 4, CompletionTokens: 2, CostUSD: 0.01},
	}
	resp, err := cli.Generate(context.Background(), GenerateRequest{
		Model:    "mock/test",
		Messages: []ChatMessage{{Role: "user", Content: "run"}},
		Tools:    []ToolDef{{Name: "unused"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != `{"summary":"done"}` || resp.StopReason != StopReasonEndTurn {
		t.Fatalf("resp %+v", resp)
	}
	if resp.Meta.PromptTokens != 4 || resp.Meta.CompletionTokens != 2 {
		t.Fatalf("meta %+v", resp.Meta)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("ToolCalls %+v", resp.ToolCalls)
	}
	if got := cli.LastRequest().Tools; len(got) != 1 || got[0].Name != "unused" {
		t.Fatalf("echoed Tools %+v", got)
	}
}

func TestMockClient_cloneDoesNotAliasRawMessage(t *testing.T) {
	t.Parallel()
	params := json.RawMessage(`{"type":"object"}`)
	args := json.RawMessage(`{"q":"go"}`)
	cli := &MockClient{Content: "ok"}
	_, err := cli.Generate(context.Background(), GenerateRequest{
		Model: "mock/alias",
		Messages: []ChatMessage{{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:        "c1",
				Name:      "search",
				Arguments: args,
			}},
		}},
		Tools: []ToolDef{{Name: "search", Parameters: params}},
	})
	if err != nil {
		t.Fatal(err)
	}
	params[0] = 'X'
	args[0] = 'X'
	got := cli.LastRequest()
	if string(got.Tools[0].Parameters) != `{"type":"object"}` {
		t.Fatalf("Parameters aliased: %s", got.Tools[0].Parameters)
	}
	if string(got.Messages[0].ToolCalls[0].Arguments) != `{"q":"go"}` {
		t.Fatalf("Arguments aliased: %s", got.Messages[0].ToolCalls[0].Arguments)
	}
	reqs := cli.Requests()
	if string(reqs[0].Tools[0].Parameters) != `{"type":"object"}` {
		t.Fatalf("Requests Parameters aliased: %s", reqs[0].Tools[0].Parameters)
	}
}

func TestMockClient_Reset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cli := &MockClient{
		Script: []MockTurn{{Content: "a"}, {Content: "b"}},
	}
	first, err := cli.Generate(ctx, GenerateRequest{Model: "m1"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Content != "a" || cli.CallCount() != 1 {
		t.Fatalf("before reset content=%q count=%d", first.Content, cli.CallCount())
	}
	cli.Reset()
	if cli.CallCount() != 0 || cli.LastRequest().Model != "" {
		t.Fatalf("after reset count=%d last=%+v", cli.CallCount(), cli.LastRequest())
	}
	again, err := cli.Generate(ctx, GenerateRequest{Model: "m2"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Content != "a" {
		t.Fatalf("after reset expected first script turn, got %q", again.Content)
	}
	if cli.LastRequest().Model != "m2" || cli.CallCount() != 1 {
		t.Fatalf("replay last=%+v count=%d", cli.LastRequest(), cli.CallCount())
	}
}

func TestMockClient_estimatesCostFromTokenUsage(t *testing.T) {
	t.Parallel()
	const model = "gpt-4o-mini"
	turns := []GenerateMeta{
		{PromptTokens: 1_000_000, CompletionTokens: 0},
		{PromptTokens: 0, CompletionTokens: 500_000},
		{PromptTokens: 100_000, CompletionTokens: 50_000},
	}
	script := make([]MockTurn, len(turns))
	var wantSum float64
	for i, meta := range turns {
		m := meta
		script[i] = MockTurn{Content: string(rune('a' + i)), Meta: &m}
		wantSum += estimateOpenAIChatCostUSD(model, meta.PromptTokens, meta.CompletionTokens)
	}
	if wantSum <= 0 {
		t.Fatal("expected non-zero B1 sum for gpt-4o-mini")
	}
	cli := &MockClient{Script: script}
	ctx := context.Background()
	var gotSum float64
	for i, meta := range turns {
		resp, err := cli.Generate(ctx, GenerateRequest{Model: model})
		if err != nil {
			t.Fatal(err)
		}
		want := estimateOpenAIChatCostUSD(model, meta.PromptTokens, meta.CompletionTokens)
		if resp.Meta.PromptTokens != meta.PromptTokens || resp.Meta.CompletionTokens != meta.CompletionTokens {
			t.Fatalf("turn %d tokens %+v", i+1, resp.Meta)
		}
		if resp.Meta.CostUSD != want {
			t.Fatalf("turn %d CostUSD %v want %v", i+1, resp.Meta.CostUSD, want)
		}
		gotSum += resp.Meta.CostUSD
	}
	if gotSum != wantSum {
		t.Fatalf("accumulated CostUSD %v want %v", gotSum, wantSum)
	}
}

func TestMockClient_explicitCostUSDNotOverwritten(t *testing.T) {
	t.Parallel()
	cli := &MockClient{
		Script: []MockTurn{{
			Content: "ok",
			Meta:    &GenerateMeta{PromptTokens: 1_000_000, CompletionTokens: 0, CostUSD: 0.02},
		}},
	}
	resp, err := cli.Generate(context.Background(), GenerateRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Meta.CostUSD != 0.02 {
		t.Fatalf("CostUSD %v, want injected 0.02 (not B1 estimate)", resp.Meta.CostUSD)
	}
}

func TestMockClient_unknownModelTokenCostStaysZero(t *testing.T) {
	t.Parallel()
	cli := &MockClient{
		Meta: &GenerateMeta{PromptTokens: 1_000_000, CompletionTokens: 1_000_000},
	}
	resp, err := cli.Generate(context.Background(), GenerateRequest{Model: "gpt-4"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Meta.CostUSD != 0 {
		t.Fatalf("CostUSD %v, want 0 for unpriced gpt-4", resp.Meta.CostUSD)
	}
}

func TestMockClient_honorsExplicitStopReasonAndErr(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("provider down")
	cli := &MockClient{
		Script: []MockTurn{
			{
				ToolCalls: []ToolCall{{
					ID:        "c1",
					Name:      "search",
					Arguments: json.RawMessage(`{}`),
				}},
				StopReason: StopReasonMaxTokens,
				Meta:       &GenerateMeta{PromptTokens: 1, CompletionTokens: 1},
			},
			{Err: wantErr},
		},
	}
	resp, err := cli.Generate(context.Background(), GenerateRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != StopReasonMaxTokens || len(resp.ToolCalls) != 1 {
		t.Fatalf("resp %+v", resp)
	}
	_, err = cli.Generate(context.Background(), GenerateRequest{Model: "m"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err %v, want %v", err, wantErr)
	}
}
