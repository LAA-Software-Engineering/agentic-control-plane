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
	RT           state.RuntimeStore
	Clock        func() time.Time
	Redaction    RedactionOptions
	logicalOrder *int
}

// NewRecorder returns a recorder backed by rt. rt must not be nil when Append is called.
func NewRecorder(rt state.RuntimeStore) *Recorder {
	return &Recorder{RT: rt, Redaction: NormalizeRedactionOptions(DefaultRedactionOptions())}
}

// WithLogicalOrder returns a recorder that stamps data_json.logicalOrder on every Append.
// YAML step index is the stable replay order for concurrent workflow branches (issue #192).
func (r *Recorder) WithLogicalOrder(order int) *Recorder {
	if r == nil {
		return nil
	}
	cp := *r
	o := order
	cp.logicalOrder = &o
	return &cp
}

func (r *Recorder) now() time.Time {
	if r != nil && r.Clock != nil {
		return r.Clock()
	}
	return time.Now().UTC()
}

// Append verifies the run exists, serializes data to JSON for data_json, then appends one event.
// stepID may be empty for run-level events. eventType must be a known [EventType]; actorType must
// be a known [ActorType].
func (r *Recorder) Append(ctx context.Context, runID, stepID string, eventType EventType, actorType ActorType, data map[string]any) (seq int64, err error) {
	if r == nil || r.RT == nil {
		return 0, errors.New("trace: nil recorder or runtime store")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return 0, errors.New("trace: empty run_id")
	}
	if err := ValidateEventType(eventType); err != nil {
		return 0, err
	}
	if err := ValidateActorType(actorType); err != nil {
		return 0, err
	}

	if _, err := r.RT.GetRun(ctx, runID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("trace: cannot append event for run %q: %w", runID, ErrRunNotFound)
		}
		return 0, fmt.Errorf("trace: get run %q: %w", runID, err)
	}

	dataJSON := "{}"
	if r.logicalOrder != nil {
		if data == nil {
			data = map[string]any{}
		} else {
			cp := make(map[string]any, len(data)+1)
			for k, v := range data {
				cp[k] = v
			}
			data = cp
		}
		data["logicalOrder"] = *r.logicalOrder
	}
	if len(data) > 0 {
		prepared := PrepareEventData(data, nil, r.Redaction)
		b, err := json.Marshal(prepared)
		if err != nil {
			return 0, fmt.Errorf("trace: marshal event data: %w", err)
		}
		dataJSON = string(b)
	}

	return r.RT.AppendTraceEvent(ctx, runID, r.now(), eventType.String(), actorType.String(), strings.TrimSpace(stepID), dataJSON)
}
