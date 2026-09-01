package models

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

// The Grok/Gemini/Kimi adapters are OpenAIClient with a provider-specific base
// URL and pricing table. These tests pin the base URL each constructor selects,
// that the registry resolves the provider type, and that CostUSD comes from the
// provider's own price rows (not the OpenAI table).

func TestRegistry_resolvesOpenAICompatibleProviders(t *testing.T) {
	cases := []struct {
		provider string
		env      string
		modelRef string
		wantID   string
		wantBase string
		wantCost string
	}{
		{"grok", "XAI_API_KEY", "grok/grok-4", "grok-4", defaultGrokBase, costProviderGrok},
		{"gemini", "GEMINI_API_KEY", "gemini/gemini-2.5-pro", "gemini-2.5-pro", defaultGeminiBase, costProviderGemini},
		{"kimi", "MOONSHOT_API_KEY", "kimi/kimi-k2", "kimi-k2", defaultKimiBase, costProviderKimi},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			t.Setenv(tc.env, "sk-test")
			g := &spec.ProjectGraph{
				Spec: spec.ProjectSpec{
					Providers: &spec.ProjectProviders{
						Models: map[string]spec.ModelProviderConfig{
							tc.provider: {Type: tc.provider, APIKeyFrom: "env:" + tc.env},
						},
					},
				},
			}
			reg := NewRegistry(g)
			cli, id, err := reg.ClientFor(tc.modelRef)
			if err != nil {
				t.Fatal(err)
			}
			if id != tc.wantID {
				t.Fatalf("model id %q, want %q", id, tc.wantID)
			}
			oc, ok := cli.(*OpenAIClient)
			if !ok {
				t.Fatalf("want *OpenAIClient, got %T", cli)
			}
			if oc.BaseURL != tc.wantBase {
				t.Fatalf("BaseURL %q, want %q", oc.BaseURL, tc.wantBase)
			}
			if oc.costProvider() != tc.wantCost {
				t.Fatalf("costProvider %q, want %q", oc.costProvider(), tc.wantCost)
			}
		})
	}
}

func TestOpenAICompatibleClient_costUsesProviderTable(t *testing.T) {
	// grok-4 is 3.00/15.00 per 1M; the OpenAI table has no such model, so a
	// non-zero cost proves the Grok pricing table drove the estimate.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1000,"completion_tokens":500}}`))
	}))
	defer srv.Close()

	c := &OpenAIClient{APIKey: "sk", BaseURL: srv.URL, HTTPClient: srv.Client(), CostProvider: costProviderGrok}
	resp, err := c.Generate(context.Background(), GenerateRequest{
		Model:    "grok-4",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := 1000.0/1e6*3.00 + 500.0/1e6*15.00
	if math.Abs(resp.Meta.CostUSD-want) > 1e-9 {
		t.Fatalf("CostUSD got %v want %v", resp.Meta.CostUSD, want)
	}
	// The same model id under the default (OpenAI) table is unknown → 0.
	if got := estimateOpenAIChatCostUSD("grok-4", 1000, 500); got != 0 {
		t.Fatalf("openai table should not price grok-4, got %v", got)
	}
}

// Gemini's OpenAI-compatible base carries a trailing slash; the request path
// must still resolve to a single "/chat/completions".
func TestGeminiClient_trailingSlashBase(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := &OpenAIClient{APIKey: "sk", BaseURL: srv.URL + "/v1beta/openai/", HTTPClient: srv.Client(), CostProvider: costProviderGemini}
	if _, err := c.Generate(context.Background(), GenerateRequest{
		Model:    "gemini-2.5-flash",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1beta/openai/chat/completions" {
		t.Fatalf("request path %q", gotPath)
	}
}
