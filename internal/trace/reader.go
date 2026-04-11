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
func (r *Reader) ListByRunID(ctx context.Context, runID string) ([]Event, error) {
	if r == nil || r.RT == nil {
		return nil, errors.New("trace: nil reader or runtime store")
	}
	return r.RT.ListTraceEventsByRunID(ctx, runID)
}
