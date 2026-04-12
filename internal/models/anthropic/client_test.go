package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_Generate_messagesAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("x-api-key") != "sk-ant-test" {
			t.Errorf("x-api-key %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != apiVersion {
			t.Errorf("anthropic-version %q", r.Header.Get("anthropic-version"))
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var req struct {
			Model    string `json:"model"`
			System   string `json:"system"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			MaxTokens int `json:"max_tokens"`
		}
		if err := json.Unmarshal(b, &req); err != nil {
			t.Fatal(err)
		}
		if req.Model != "claude-sonnet-4-20250514" {
			t.Errorf("model %q", req.Model)
		}
		if req.System != "Be brief." {
			t.Errorf("system %q", req.System)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != `{"q":1}` {
			t.Fatalf("messages %+v", req.Messages)
		}
		if req.MaxTokens != defaultMaxTok {
			t.Errorf("max_tokens %d", req.MaxTokens)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"{\"ok\":true}"}],"usage":{"input_tokens":10,"output_tokens":20}}`))
	}))
	defer srv.Close()

	c := &Client{APIKey: "sk-ant-test", BaseURL: srv.URL, HTTPClient: srv.Client()}
	text, inT, outT, _, err := c.Generate(context.Background(), "claude-sonnet-4-20250514", "Be brief.", []ChatMessage{
		{Role: "user", Content: `{"q":1}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != `{"ok":true}` {
		t.Fatalf("text %q", text)
	}
	if inT != 10 || outT != 20 {
		t.Fatalf("usage in=%d out=%d", inT, outT)
	}
}

func TestClient_Generate_concatTextBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}`))
	}))
	defer srv.Close()

	c := &Client{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()}
	text, _, _, _, err := c.Generate(context.Background(), "m", "", []ChatMessage{{Role: "user", Content: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if text != "ab" {
		t.Fatalf("got %q", text)
	}
}

func TestClient_Generate_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`))
	}))
	defer srv.Close()

	c := &Client{APIKey: "bad", BaseURL: srv.URL, HTTPClient: srv.Client()}
	_, _, _, _, err := c.Generate(context.Background(), "m", "", []ChatMessage{{Role: "user", Content: "x"}})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("got %v", err)
	}
}
