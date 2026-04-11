package tools

import "time"

// normalizeResponse ensures non-nil Output and fills default Meta when zero.
func normalizeResponse(output map[string]any, meta ToolCallMeta, start time.Time) ToolCallResponse {
	if output == nil {
		output = map[string]any{}
	}
	if meta.DurationMs == 0 && !start.IsZero() {
		meta.DurationMs = time.Since(start).Milliseconds()
	}
	return ToolCallResponse{Output: output, Meta: meta}
}
