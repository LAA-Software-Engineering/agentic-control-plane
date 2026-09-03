package models

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Terfyn/terfyn/internal/models/anthropic"
	"github.com/Terfyn/terfyn/internal/spec"
)

// anthropicClient adapts [anthropic.Client] to [ModelClient] (issue #69, #158).
type anthropicClient struct {
	inner *anthropic.Client
}

// NewAnthropicClientFromConfig builds a client using apiKeyFrom (e.g. env:ANTHROPIC_API_KEY),
// and optionally workspaceIdFrom (e.g. env:ANTHROPIC_WORKSPACE_ID) for the
// anthropic-workspace-id header required by identity-linked keys.
func NewAnthropicClientFromConfig(cfg spec.ModelProviderConfig) (*anthropicClient, error) {
	key, err := ResolveAPIKeyFrom(cfg.APIKeyFrom)
	if err != nil {
		return nil, err
	}
	var workspaceID string
	if cfg.WorkspaceIDFrom != "" {
		workspaceID, err = ResolveAPIKeyFrom(cfg.WorkspaceIDFrom)
		if err != nil {
			return nil, err
		}
	}
	return &anthropicClient{
		inner: &anthropic.Client{APIKey: key, WorkspaceID: workspaceID, HTTPClient: http.DefaultClient},
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
	resp := mapFromAnthropicResponse(out)
	resp.Meta.CostUSD = estimateAnthropicCostUSD(req.Model, resp.Meta.PromptTokens, resp.Meta.CompletionTokens)
	return resp, nil
}
