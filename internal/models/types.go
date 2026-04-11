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

// GenerateMeta holds MVP placeholders for duration and cost (§13.2 style).
type GenerateMeta struct {
	DurationMs int64
	CostUSD    float64
}
