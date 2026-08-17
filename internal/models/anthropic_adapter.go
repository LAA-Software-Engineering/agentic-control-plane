package models

import (
	"context"
	"fmt"
	"net/http"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models/anthropic"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// anthropicClient adapts [anthropic.Client] to [ModelClient] (issue #69, #158).
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

func (a *anthropicClient) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	if a == nil || a.inner == nil {
		return GenerateResponse{}, fmt.Errorf("models: anthropic client not configured")
	}
	innerReq, err := mapToAnthropicRequest(req)
	if err != nil {
		return GenerateResponse{}, err
	}
	out, err := a.inner.Generate(ctx, innerReq)
	if err != nil {
		return GenerateResponse{}, err
	}
	return mapFromAnthropicResponse(out), nil
}
