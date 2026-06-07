package trace

// Legacy dot-notation → canonical mappings are also applied by SQLite migration 006
// (migrations/sqlite/006_trace_event_taxonomy.sql). Keep both in sync when TaxonomyVersion changes.
var legacyEventTypeMap = map[string]string{
	"run.started":        string(EventRunStarted),
	"run.finished":       string(EventRunFinished),
	"run.interrupted":    string(EventRunError),
	"run.resumed":        string(EventRunStarted),
	"step.started":       string(EventToolSelection),
	"step.finished":      string(EventToolExecution),
	"step.failed":        string(EventRunError),
	"tool.called":        string(EventToolSelection),
	"tool.completed":     string(EventToolExecution),
	"model.called":       string(EventLLMCompletion),
	"model.completed":    string(EventLLMCompletion),
	"policy.denied":      string(EventSystemError),
	"approval.requested": string(EventHitlRequestCreated),
	"approval.resolved":  string(EventHitlDecisionSubmitted),
}

// LegacyActorTypeForEvent assigns actor_type when backfilling legacy rows without the column.
func LegacyActorTypeForEvent(eventType string) ActorType {
	switch EventType(eventType) {
	case EventHitlDecisionSubmitted:
		return ActorUser
	case EventHitlRequestCreated, EventSystemError, EventRunError:
		return ActorSystem
	default:
		return ActorAgent
	}
}

// normalizeLegacyEventType maps a legacy dot-notation type to snake_case when recognized.
func normalizeLegacyEventType(s string) (string, bool) {
	if mapped, ok := legacyEventTypeMap[s]; ok {
		return mapped, true
	}
	et := EventType(s)
	if et.Known() {
		return s, true
	}
	return s, false
}

// NormalizeStoredEventType returns the canonical event type string for a persisted row,
// translating legacy dot-notation values on read.
func NormalizeStoredEventType(raw string) string {
	if mapped, ok := normalizeLegacyEventType(raw); ok {
		return mapped
	}
	return raw
}

// LegacyEventTypeMappings returns a copy of legacy dot-notation → canonical type mappings
// applied on read and by migration 006. Tests use this to verify SQL and Go stay aligned.
func LegacyEventTypeMappings() map[string]string {
	out := make(map[string]string, len(legacyEventTypeMap))
	for k, v := range legacyEventTypeMap {
		out[k] = v
	}
	return out
}
