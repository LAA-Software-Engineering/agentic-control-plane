package inspect

import (
	"encoding/json"
	"time"

	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/trace"
)

func stepsToRecords(steps []state.RunStep) []StepRecord {
	out := make([]StepRecord, 0, len(steps))
	for _, s := range steps {
		rec := StepRecord{
			StepID:  s.StepID,
			Status:  s.Status,
			CostUsd: s.CostUSD,
		}
		if s.StartedAt != nil {
			rec.StartedAt = s.StartedAt.UTC().Format(time.RFC3339Nano)
		}
		if s.FinishedAt != nil {
			rec.FinishedAt = s.FinishedAt.UTC().Format(time.RFC3339Nano)
		}
		if s.InputJSON != "" {
			rec.Input = json.RawMessage(s.InputJSON)
		}
		if s.OutputJSON != "" {
			rec.Output = json.RawMessage(s.OutputJSON)
		}
		if s.ErrorText != "" {
			rec.Error = s.ErrorText
		}
		out = append(out, rec)
	}
	return out
}

func checkpointsToRecords(cps []state.RunCheckpoint, redaction trace.RedactionOptions) []CheckpointRecord {
	out := make([]CheckpointRecord, 0, len(cps))
	for _, cp := range cps {
		ctxJ := cp.ContextJSON
		if ctxJ == "" {
			ctxJ = "{}"
		}
		// The checkpoint is stored raw so the interpreter can dispatch the pending call on resume, but a
		// read surface must not serve a token/password/authorization in clear — the trace masks the same
		// values (issue #408). Redact at display: every completed step's Output and the pending gate's args.
		ctxJ = redactCheckpointContext(ctxJ, redaction)
		out = append(out, CheckpointRecord{
			Seq:       cp.Seq,
			StepIndex: cp.StepIndex,
			StepID:    cp.StepID,
			Status:    cp.Status,
			CreatedAt: cp.CreatedAt.UTC().Format(time.RFC3339Nano),
			Context:   json.RawMessage(ctxJ),
		})
	}
	return out
}

// redactCheckpointContext masks sensitive values in a checkpoint context JSON for display, preserving
// structure. Best-effort: malformed JSON is returned unchanged (checkpoint context is always valid
// JSON we wrote, so this is a defensive fallback).
func redactCheckpointContext(ctxJSON string, redaction trace.RedactionOptions) string {
	var v any
	if err := json.Unmarshal([]byte(ctxJSON), &v); err != nil {
		return ctxJSON
	}
	b, err := json.Marshal(trace.RedactValue(v, redaction))
	if err != nil {
		return ctxJSON
	}
	return string(b)
}
