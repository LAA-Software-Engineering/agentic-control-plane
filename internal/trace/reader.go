package trace

import (
	"context"
	"errors"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// Reader loads trace events from [state.RuntimeStore] (read side for logs / inspect).
type Reader struct {
	RT state.RuntimeStore
}

// NewReader returns a reader backed by rt.
func NewReader(rt state.RuntimeStore) *Reader {
	return &Reader{RT: rt}
}

// ListByRunID returns events for runID ordered by seq ascending.
// Legacy dot-notation type strings are normalized to the closed taxonomy on read (issue #115).
func (r *Reader) ListByRunID(ctx context.Context, runID string) ([]Event, error) {
	if r == nil || r.RT == nil {
		return nil, errors.New("trace: nil reader or runtime store")
	}
	events, err := r.RT.ListTraceEventsByRunID(ctx, runID)
	if err != nil {
		return nil, err
	}
	for i := range events {
		events[i].Type = NormalizeStoredEventType(events[i].Type)
		if events[i].ActorType == "" {
			events[i].ActorType = string(LegacyActorTypeForEvent(events[i].Type))
		}
	}
	return events, nil
}

// NormalizeEvents applies legacy type normalization and actor_type backfill for loaded rows.
func NormalizeEvents(events []state.TraceEvent) []state.TraceEvent {
	for i := range events {
		events[i].Type = NormalizeStoredEventType(events[i].Type)
		if events[i].ActorType == "" {
			events[i].ActorType = string(LegacyActorTypeForEvent(events[i].Type))
		}
	}
	return events
}
