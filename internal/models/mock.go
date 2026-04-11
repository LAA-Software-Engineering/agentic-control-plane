package models

import "context"

// MockClient returns a fixed response for tests and offline agent steps (design doc §12.2 F MVP).
type MockClient struct {
	// Content is returned as GenerateResponse.Content verbatim (often JSON for structured output tests).
	Content string
	Meta    *GenerateMeta
}

// Generate returns deterministic output without calling the network.
func (m *MockClient) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	_ = ctx
	_ = req
	meta := GenerateMeta{DurationMs: 1, CostUSD: 0.001}
	if m.Meta != nil {
		meta = *m.Meta
	}
	return GenerateResponse{Content: m.Content, Meta: meta}, nil
}
