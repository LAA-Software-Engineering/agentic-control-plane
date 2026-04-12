package models

import "context"

// ModelClient invokes a chat-capable model (design doc §12.2 F).
type ModelClient interface {
	Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
}

// ChatMessage is one turn in the prompt payload (MVP text only).
type ChatMessage struct {
	Role    string
	Content string
}

// GenerateRequest is a minimal generation call (OpenAI-style chat).
type GenerateRequest struct {
	Model    string
	Messages []ChatMessage
}

// GenerateResponse carries model text output and placeholder accounting (issue #17).
type GenerateResponse struct {
	// Content is assistant message text. For structured agents, callers often expect JSON in this string.
	Content string
	Meta    GenerateMeta
}

// GenerateMeta holds duration and cost accounting (§13.2 style).
// CostUSD is 0 for the mock client unless injected. For OpenAI chat completions it is a rough
// estimate from usage × published per-million token rates when the model is recognized.
type GenerateMeta struct {
	DurationMs int64
	CostUSD    float64
}
