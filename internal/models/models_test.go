package models

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models/anthropic"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// Agent-step style: mock returns JSON that unmarshals into a fixed schema (issue #17 acceptance).
func TestMockClient_usableForAgentStructuredOutput(t *testing.T) {
	ctx := context.Background()
	cli := &MockClient{
		Content: `{"summary":"done","findings":[{"id":"f1"}]}`,
		Meta:    &GenerateMeta{DurationMs: 42, CostUSD: 0.02},
	}
	resp, err := cli.Generate(ctx, GenerateRequest{
		Model:    "mock/test",
		Messages: []ChatMessage{{Role: "user", Content: "run"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Summary  string `json:"summary"`
		Findings []struct {
			ID string `json:"id"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &decoded); err != nil {
		t.Fatalf("decode mock output: %v", err)
	}
	if decoded.Summary != "done" || len(decoded.Findings) != 1 || decoded.Findings[0].ID != "f1" {
		t.Fatalf("got %+v", decoded)
	}
	if resp.Meta.DurationMs != 42 || resp.Meta.CostUSD != 0.02 {
		t.Fatalf("meta %+v", resp.Meta)
	}
}

func TestRegistry_unknownProviderNamespace(t *testing.T) {
	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Providers: &spec.ProjectProviders{
				Models: map[string]spec.ModelProviderConfig{
					"openai": {Type: "openai", APIKeyFrom: "env:OPENAI_API_KEY"},
				},
			},
		},
	}
	reg := NewRegistry(g)
	_, _, err := reg.ClientFor("anthropic/claude-3")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown provider namespace") || !strings.Contains(err.Error(), "anthropic") {
		t.Fatalf("got %v", err)
	}
}

func TestRegistry_modelRefFormat(t *testing.T) {
	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Providers: &spec.ProjectProviders{
				Models: map[string]spec.ModelProviderConfig{
					"openai": {Type: "openai", APIKeyFrom: "env:OPENAI_API_KEY"},
				},
			},
		},
	}
	reg := NewRegistry(g)
	_, _, err := reg.ClientFor("badref")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "namespace/model_id") {
		t.Fatalf("got %v", err)
	}
}

func TestRegistry_resolvesAnthropicAndModelID(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Providers: &spec.ProjectProviders{
				Models: map[string]spec.ModelProviderConfig{
					"anthropic": {Type: "anthropic", APIKeyFrom: "env:ANTHROPIC_API_KEY"},
				},
			},
		},
	}
	reg := NewRegistry(g)
	cli, id, err := reg.ClientFor("anthropic/claude-sonnet-4-20250514")
	if err != nil {
		t.Fatal(err)
	}
	if id != "claude-sonnet-4-20250514" {
		t.Fatalf("model id %q", id)
	}
	if _, ok := cli.(*anthropicClient); !ok {
		t.Fatalf("want *anthropicClient, got %T", cli)
	}
}

func TestRegistry_resolvesOpenAIAndModelID(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Providers: &spec.ProjectProviders{
				Models: map[string]spec.ModelProviderConfig{
					"openai": {Type: "openai", APIKeyFrom: "env:OPENAI_API_KEY"},
				},
			},
		},
	}
	reg := NewRegistry(g)
	cli, id, err := reg.ClientFor("openai/gpt-4.1")
	if err != nil {
		t.Fatal(err)
	}
	if id != "gpt-4.1" {
		t.Fatalf("model id %q", id)
	}
	if _, ok := cli.(*OpenAIClient); !ok {
		t.Fatalf("want *OpenAIClient, got %T", cli)
	}
}

func TestAnthropicClient_Generate_mapsSystemAndUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var req struct {
			System   string `json:"system"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(b, &req); err != nil {
			t.Fatal(err)
		}
		if req.System != "do json" {
			t.Errorf("system %q", req.System)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
			t.Fatalf("messages %+v", req.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"{\"k\":1}"}]}`))
	}))
	defer srv.Close()

	a := &anthropicClient{inner: &anthropic.Client{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()}}
	resp, err := a.Generate(context.Background(), GenerateRequest{
		Model: "claude-test",
		Messages: []ChatMessage{
			{Role: "system", Content: "do json"},
			{Role: "user", Content: `{}`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != `{"k":1}` {
		t.Fatalf("content %q", resp.Content)
	}
}

func TestOpenAIClient_Generate_usesChatCompletions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer sk-mock" {
			t.Errorf("Authorization %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello"}}],"usage":{"prompt_tokens":1000,"completion_tokens":500}}`))
	}))
	defer srv.Close()

	c := &OpenAIClient{APIKey: "sk-mock", BaseURL: srv.URL + "/v1", HTTPClient: srv.Client()}
	resp, err := c.Generate(context.Background(), GenerateRequest{
		Model: "gpt-4o-mini",
		Messages: []ChatMessage{
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hello" {
		t.Fatalf("content %q", resp.Content)
	}
	// 1000/1e6*0.15 + 500/1e6*0.60 = 0.00045
	want := 1000.0/1e6*0.15 + 500.0/1e6*0.60
	if math.Abs(resp.Meta.CostUSD-want) > 1e-9 {
		t.Fatalf("CostUSD got %v want %v", resp.Meta.CostUSD, want)
	}
	if resp.Meta.PromptTokens != 1000 || resp.Meta.CompletionTokens != 500 {
		t.Fatalf("token usage got prompt=%d completion=%d", resp.Meta.PromptTokens, resp.Meta.CompletionTokens)
	}
}

func TestOpenAIClient_Generate_unknownModel_zeroCost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"x"}}],"usage":{"prompt_tokens":100,"completion_tokens":100}}`))
	}))
	defer srv.Close()

	c := &OpenAIClient{APIKey: "sk-mock", BaseURL: srv.URL + "/v1", HTTPClient: srv.Client()}
	resp, err := c.Generate(context.Background(), GenerateRequest{
		Model: "unknown-model-xyz",
		Messages: []ChatMessage{
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Meta.CostUSD != 0 {
		t.Fatalf("CostUSD %v", resp.Meta.CostUSD)
	}
}

func TestResolveAPIKeyFrom_env(t *testing.T) {
	t.Setenv("MY_KEY", "abc")
	got, err := ResolveAPIKeyFrom("env:MY_KEY")
	if err != nil || got != "abc" {
		t.Fatalf("%q %v", got, err)
	}
	_, err = ResolveAPIKeyFrom("env:MISSING_XYZ_404")
	if err == nil {
		t.Fatal("expected error")
	}
}
