package models

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models/anthropic"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// anthropicClient adapts [anthropic.Client] to [ModelClient] (issue #69).
type anthropicClient struct {
	inner *anthropic.Client
}

// NewAnthropicClientFromConfig builds a client using apiKeyFrom (e.g. env:ANTHROPIC_API_KEY).
func NewAnthropicClientFromConfig(cfg spec.ModelProviderConfig) (*anthropicClient, error) {
	key, err := ResolveAPIKeyFrom(cfg.APIKeyFrom)
	if err != nil {
		return nil, err
	}
	return &anthropicClient{
		inner: &anthropic.Client{APIKey: key, HTTPClient: http.DefaultClient},
	}, nil
}

// splitAnthropicMessages maps engine-style chat (system + user/assistant) to Anthropic's
// top-level system string and user|assistant message list.
func splitAnthropicMessages(msgs []ChatMessage) (system string, out []anthropic.ChatMessage, err error) {
	var sys []string
	for _, m := range msgs {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		switch role {
		case "system":
			sys = append(sys, m.Content)
		case "user", "assistant":
			out = append(out, anthropic.ChatMessage{Role: role, Content: m.Content})
		default:
			return "", nil, fmt.Errorf("models: anthropic does not support message role %q (use system, user, or assistant)", m.Role)
		}
	}
	return strings.Join(sys, "\n\n"), out, nil
}

func (a *anthropicClient) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	if a == nil || a.inner == nil {
		return GenerateResponse{}, fmt.Errorf("models: anthropic client not configured")
	}
	system, msgs, err := splitAnthropicMessages(req.Messages)
	if err != nil {
		return GenerateResponse{}, err
	}
	text, _, _, durationMs, err := a.inner.Generate(ctx, req.Model, system, msgs)
	if err != nil {
		return GenerateResponse{}, err
	}
	return GenerateResponse{
		Content: text,
		Meta:    GenerateMeta{DurationMs: durationMs, CostUSD: 0},
	}, nil
}
