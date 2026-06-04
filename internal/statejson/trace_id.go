package statejson

import (
	"encoding/json"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

type traceIDPayload struct {
	TraceID    string `json:"trace_id"`
	TraceId    string `json:"traceId"`
	TraceIDAlt string `json:"traceID"`
}

// ParseTraceID returns an OTel trace id from event data_json when present (issue #108).
func ParseTraceID(dataJSON string) string {
	if dataJSON == "" {
		return ""
	}
	var p traceIDPayload
	if err := json.Unmarshal([]byte(dataJSON), &p); err != nil {
		return ""
	}
	for _, s := range []string{p.TraceID, p.TraceId, p.TraceIDAlt} {
		if s != "" {
			return s
		}
	}
	return ""
}

// TraceIDFromEvents scans events for the first non-empty trace id in payloads.
func TraceIDFromEvents(events []state.TraceEvent) string {
	for _, e := range events {
		if id := ParseTraceID(e.DataJSON); id != "" {
			return id
		}
	}
	return ""
}
