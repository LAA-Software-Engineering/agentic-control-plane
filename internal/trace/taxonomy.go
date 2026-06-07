package trace

import (
	"fmt"
	"sort"
	"strings"
)

// TaxonomyVersion is bumped when the closed EventType or ActorType vocabulary changes.
// Older SQLite databases continue to load rows with unknown type strings without error.
const TaxonomyVersion = 1

// EventType is a closed, versioned audit/trace event identifier (issue #115).
type EventType string

// Closed event type vocabulary (TaxonomyVersion 1).
const (
	EventRunStarted            EventType = "run_started"
	EventRunFinished           EventType = "run_finished"
	EventRunError              EventType = "run_error"
	EventLLMCompletion         EventType = "llm_completion"
	EventToolSelection         EventType = "tool_selection"
	EventToolExecution         EventType = "tool_execution"
	EventHitlRequestCreated    EventType = "hitl_request_created"
	EventHitlDecisionSubmitted EventType = "hitl_decision_submitted"
	EventHitlResolutionApplied EventType = "hitl_resolution_applied"
	EventMemoryRead            EventType = "memory_read"
	EventMemoryWrite           EventType = "memory_write"
	EventSystemError           EventType = "system_error"
)

// ActorType identifies who initiated a trace event (issue #115, pairs with actor_id from #111).
type ActorType string

// Actor type vocabulary.
const (
	ActorUser   ActorType = "user"
	ActorAgent  ActorType = "agent"
	ActorSystem ActorType = "system"
)

var allEventTypes = []EventType{
	EventRunStarted,
	EventRunFinished,
	EventRunError,
	EventLLMCompletion,
	EventToolSelection,
	EventToolExecution,
	EventHitlRequestCreated,
	EventHitlDecisionSubmitted,
	EventHitlResolutionApplied,
	EventMemoryRead,
	EventMemoryWrite,
	EventSystemError,
}

var knownEventTypes map[EventType]struct{}
var allActorTypes = []ActorType{ActorUser, ActorAgent, ActorSystem}
var knownActorTypes map[ActorType]struct{}

func init() {
	knownEventTypes = make(map[EventType]struct{}, len(allEventTypes))
	for _, et := range allEventTypes {
		knownEventTypes[et] = struct{}{}
	}
	knownActorTypes = make(map[ActorType]struct{}, len(allActorTypes))
	for _, at := range allActorTypes {
		knownActorTypes[at] = struct{}{}
	}
}

// String returns the persisted event type string.
func (e EventType) String() string { return string(e) }

// String returns the persisted actor type string.
func (a ActorType) String() string { return string(a) }

// Known reports whether et is part of the current closed vocabulary.
func (e EventType) Known() bool {
	_, ok := knownEventTypes[e]
	return ok
}

// Known reports whether a is part of the closed actor vocabulary.
func (a ActorType) Known() bool {
	_, ok := knownActorTypes[a]
	return ok
}

// AllEventTypes returns the sorted closed event type list for CLI validation and docs.
func AllEventTypes() []EventType {
	out := append([]EventType(nil), allEventTypes...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// AllEventTypeStrings returns sorted event type strings for error messages and flags.
func AllEventTypeStrings() []string {
	types := AllEventTypes()
	out := make([]string, len(types))
	for i, et := range types {
		out[i] = et.String()
	}
	return out
}

// AllActorTypes returns the closed actor type list.
func AllActorTypes() []ActorType {
	out := append([]ActorType(nil), allActorTypes...)
	return out
}

// ParseEventType parses s into an EventType. Unknown values are returned as-is with known=false
// so loaders tolerate newer schema versions.
func ParseEventType(s string) (EventType, bool) {
	et := EventType(strings.TrimSpace(s))
	if et == "" {
		return "", false
	}
	if normalized, ok := normalizeLegacyEventType(string(et)); ok {
		et = EventType(normalized)
	}
	return et, et.Known()
}

// ParseActorType parses s into an ActorType. Unknown values are returned as-is with known=false.
func ParseActorType(s string) (ActorType, bool) {
	at := ActorType(strings.TrimSpace(s))
	if at == "" {
		return "", false
	}
	return at, at.Known()
}

// ValidateEventType returns an error when et is empty or not in the closed vocabulary.
func ValidateEventType(et EventType) error {
	if strings.TrimSpace(string(et)) == "" {
		return fmt.Errorf("trace: empty event type")
	}
	if !et.Known() {
		return fmt.Errorf("trace: unknown event type %q (known: %s)", et, strings.Join(AllEventTypeStrings(), ", "))
	}
	return nil
}

// ValidateActorType returns an error when at is empty or not in the closed vocabulary.
func ValidateActorType(at ActorType) error {
	if strings.TrimSpace(string(at)) == "" {
		return fmt.Errorf("trace: empty actor type")
	}
	if !at.Known() {
		return fmt.Errorf("trace: unknown actor type %q", at)
	}
	return nil
}
