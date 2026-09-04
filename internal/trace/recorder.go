package trace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Terfyn/terfyn/internal/state"
)

// ErrRunNotFound is returned when appending events for a run_id that has no row in runs.
var ErrRunNotFound = errors.New("trace: run not found")

// StreamEvent is one appended event surfaced live to a [Recorder.Sink] (issue #450). Data is the
// REDACTED event data (identical to what is stored), so a live stream never reveals a secret the
// store would redact.
type StreamEvent struct {
	RunID  string
	StepID string
	Type   EventType
	Actor  ActorType
	Data   map[string]any
	Seq    int64
	Time   time.Time
}

// EventSink receives each event as it is appended. Set it on a [Recorder] to stream events live
// (e.g. `terfyn run --verbose` renders one line per event to stderr). Nil disables streaming — the
// default, so no plumbing changes for callers that do not opt in.
type EventSink func(StreamEvent)

// Recorder appends trace_events rows via [state.RuntimeStore] (design doc §12.2 I, §14.2).
type Recorder struct {
	RT        state.RuntimeStore
	Clock     func() time.Time
	Redaction RedactionOptions
	// Sink, when non-nil, is called with every successfully-appended event (redacted), in addition
	// to persisting it — the live-stream hook for --verbose (#450). It never changes what is stored.
	Sink      EventSink
	callStack []string
}

// NewRecorder returns a recorder backed by rt. rt must not be nil when Append is called.
func NewRecorder(rt state.RuntimeStore) *Recorder {
	return &Recorder{RT: rt, Redaction: NormalizeRedactionOptions(DefaultRedactionOptions())}
}

// WithCallStack stamps data_json.callStack / workflow on nested subworkflow events (issue #194).
func (r *Recorder) WithCallStack(stack []string) *Recorder {
	if r == nil {
		return nil
	}
	cp := *r
	cp.callStack = append([]string(nil), stack...)
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
	if len(r.callStack) > 0 {
		if data == nil {
			data = map[string]any{}
		} else {
			cp := make(map[string]any, len(data)+2)
			for k, v := range data {
				cp[k] = v
			}
			data = cp
		}
		data["callStack"] = append([]string(nil), r.callStack...)
		data["workflow"] = r.callStack[len(r.callStack)-1]
	}
	var prepared map[string]any
	if len(data) > 0 {
		prepared = PrepareEventData(data, nil, r.Redaction)
		b, err := json.Marshal(prepared)
		if err != nil {
			return 0, fmt.Errorf("trace: marshal event data: %w", err)
		}
		dataJSON = string(b)
	}

	ts := r.now()
	seq, err = r.RT.AppendTraceEvent(ctx, runID, ts, eventType.String(), actorType.String(), strings.TrimSpace(stepID), dataJSON)
	if err == nil && r.Sink != nil {
		// Best-effort live stream (--verbose #450): emit the REDACTED data so the stream matches
		// storage. It never affects what is persisted or the returned seq/err.
		r.Sink(StreamEvent{
			RunID:  runID,
			StepID: strings.TrimSpace(stepID),
			Type:   eventType,
			Actor:  actorType,
			Data:   prepared,
			Seq:    seq,
			Time:   ts,
		})
	}
	return seq, err
}
