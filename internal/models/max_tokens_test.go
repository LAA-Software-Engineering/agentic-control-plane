package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// The Anthropic adapter forwards GenerateRequest.MaxTokens to the inner Request (issue #514).
func TestMapToAnthropicRequest_maxTokens(t *testing.T) {
	out, err := mapToAnthropicRequest(GenerateRequest{
		Model:     "claude-opus-4-8",
		Messages:  []ChatMessage{{Role: "user", Content: "hi"}},
		MaxTokens: 32000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.MaxTokens != 32000 {
		t.Fatalf("MaxTokens = %d, want 32000", out.MaxTokens)
	}
}

// The OpenAI adapter emits max_tokens when set, and omits it when unset (leaving the API default).
func TestBuildOpenAIChatPayload_maxTokens(t *testing.T) {
	body, err := buildOpenAIChatPayload(GenerateRequest{
		Model:     "gpt-4o",
		Messages:  []ChatMessage{{Role: "user", Content: "hi"}},
		MaxTokens: 32000,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		MaxTokens int `json:"max_tokens"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.MaxTokens != 32000 {
		t.Fatalf("max_tokens = %d, want 32000", got.MaxTokens)
	}

	body, err = buildOpenAIChatPayload(GenerateRequest{Model: "gpt-4o", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "max_tokens") {
		t.Fatalf("max_tokens present when unset: %s", body)
	}
}
