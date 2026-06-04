package trace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// ErrRunNotFound is returned when appending events for a run_id that has no row in runs.
var ErrRunNotFound = errors.New("trace: run not found")

// Recorder appends trace_events rows via [state.RuntimeStore] (design doc §12.2 I, §14.2).
type Recorder struct {
	RT        state.RuntimeStore
	Clock     func() time.Time
	Redaction RedactionOptions
}

// NewRecorder returns a recorder backed by rt. rt must not be nil when Append is called.
func NewRecorder(rt state.RuntimeStore) *Recorder {
	return &Recorder{RT: rt, Redaction: DefaultRedactionOptions()}
}

func (r *Recorder) now() time.Time {
	if r != nil && r.Clock != nil {
		return r.Clock()
	}
	return time.Now().UTC()
}

// Append verifies the run exists, serializes data to JSON for data_json, then appends one event.
// stepID may be empty for run-level events.
func (r *Recorder) Append(ctx context.Context, runID, stepID, typ string, data map[string]any) (seq int64, err error) {
	if r == nil || r.RT == nil {
		return 0, errors.New("trace: nil recorder or runtime store")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return 0, errors.New("trace: empty run_id")
	}
	typ = strings.TrimSpace(typ)
	if typ == "" {
		return 0, errors.New("trace: empty event type")
	}

	if _, err := r.RT.GetRun(ctx, runID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("trace: cannot append event for run %q: %w", runID, ErrRunNotFound)
		}
		return 0, fmt.Errorf("trace: get run %q: %w", runID, err)
	}

	dataJSON := "{}"
	if len(data) > 0 {
		prepared, err := PrepareEventData(data, nil, NormalizeRedactionOptions(r.Redaction))
		if err != nil {
			return 0, fmt.Errorf("trace: prepare event data: %w", err)
		}
		b, err := json.Marshal(prepared)
		if err != nil {
			return 0, fmt.Errorf("trace: marshal event data: %w", err)
		}
		dataJSON = string(b)
	}

	return r.RT.AppendTraceEvent(ctx, runID, r.now(), typ, strings.TrimSpace(stepID), dataJSON)
}
