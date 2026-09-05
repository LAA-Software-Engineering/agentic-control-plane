package models

import (
	"encoding/json"
	"fmt"
)

// normalizeStructuredOutputSchema validates that a [ResponseFormat.Schema] is a non-empty JSON object
// before it is sent to a provider as a structured-output constraint, and returns it verbatim. It is
// the provider-neutral guard shared by the Anthropic and OpenAI adapters (issue #510); each provider
// still enforces its own JSON-Schema subset server-side and rejects a schema it cannot compile.
func normalizeStructuredOutputSchema(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("models: structured output schema is empty")
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("models: structured output schema is not JSON")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("models: structured output schema: %w", err)
	}
	if _, ok := v.(map[string]any); !ok {
		return nil, fmt.Errorf("models: structured output schema must be a JSON object")
	}
	return raw, nil
}
