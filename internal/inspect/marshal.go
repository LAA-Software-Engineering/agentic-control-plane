package inspect

import (
	"encoding/json"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
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

func checkpointsToRecords(cps []state.RunCheckpoint) []CheckpointRecord {
	out := make([]CheckpointRecord, 0, len(cps))
	for _, cp := range cps {
		ctxJ := cp.ContextJSON
		if ctxJ == "" {
			ctxJ = "{}"
		}
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
