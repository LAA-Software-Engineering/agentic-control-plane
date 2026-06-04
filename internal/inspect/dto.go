package inspect

import (
	"encoding/json"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// EventRecord is one trace event in API responses (same JSON shape as agentctl logs -o json).
type EventRecord struct {
	Seq       int64           `json:"seq"`
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	StepID    string          `json:"stepId,omitempty"`
	Data      json.RawMessage `json:"data"`
}

// RunRecord is one run row in list/detail API responses (aligned with agentctl logs -o json).
type RunRecord struct {
	RunID        string          `json:"runId"`
	Workflow     string          `json:"workflow"`
	Env          string          `json:"env"`
	Status       string          `json:"status"`
	StartedAt    string          `json:"startedAt"`
	FinishedAt   string          `json:"finishedAt,omitempty"`
	TotalCostUsd float64         `json:"totalCostUsd"`
	Input        json.RawMessage `json:"input"`
	Output       json.RawMessage `json:"output,omitempty"`
	Error        string          `json:"error,omitempty"`
}

// StepRecord is one run_steps row in run detail responses.
type StepRecord struct {
	StepID     string          `json:"stepId"`
	Status     string          `json:"status"`
	StartedAt  string          `json:"startedAt,omitempty"`
	FinishedAt string          `json:"finishedAt,omitempty"`
	CostUsd    float64         `json:"costUsd"`
	Input      json.RawMessage `json:"input,omitempty"`
	Output     json.RawMessage `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// CheckpointRecord is one run_checkpoints row.
type CheckpointRecord struct {
	Seq       int64           `json:"seq"`
	StepIndex int             `json:"stepIndex"`
	StepID    string          `json:"stepId"`
	Status    string          `json:"status"`
	CreatedAt string          `json:"createdAt"`
	Context   json.RawMessage `json:"context"`
}

// AppliedResourceRecord matches agentctl state list JSON resource entries.
type AppliedResourceRecord struct {
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	Env                string `json:"env"`
	SpecHash           string `json:"specHash"`
	AppliedAt          string `json:"appliedAt"`
	NormalizedSpecJSON string `json:"normalizedSpecJson"`
}

func runsToRecords(runs []state.Run) []RunRecord {
	out := make([]RunRecord, 0, len(runs))
	for _, r := range runs {
		out = append(out, runToRecord(r))
	}
	return out
}

func runToRecord(r state.Run) RunRecord {
	in := r.InputJSON
	if in == "" {
		in = "{}"
	}
	rec := RunRecord{
		RunID:        r.RunID,
		Workflow:     r.WorkflowName,
		Env:          r.Env,
		Status:       r.Status,
		StartedAt:    r.StartedAt.UTC().Format(time.RFC3339Nano),
		TotalCostUsd: r.TotalCostUSD,
		Input:        json.RawMessage(in),
	}
	if r.FinishedAt != nil {
		rec.FinishedAt = r.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	if r.OutputJSON != "" {
		rec.Output = json.RawMessage(r.OutputJSON)
	}
	if r.ErrorText != "" {
		rec.Error = r.ErrorText
	}
	return rec
}

func traceEventsToRecords(events []state.TraceEvent) []EventRecord {
	out := make([]EventRecord, 0, len(events))
	for _, e := range events {
		rec := EventRecord{
			Seq:       e.Seq,
			Timestamp: e.Timestamp.UTC().Format(time.RFC3339Nano),
			Type:      e.Type,
			StepID:    e.StepID,
		}
		if e.DataJSON != "" {
			rec.Data = json.RawMessage(e.DataJSON)
		} else {
			rec.Data = json.RawMessage("{}")
		}
		out = append(out, rec)
	}
	return out
}

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

func appliedResourcesToRecords(rows []state.AppliedResource) []AppliedResourceRecord {
	out := make([]AppliedResourceRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, AppliedResourceRecord{
			Kind:               r.Kind,
			Name:               r.Name,
			Env:                r.Env,
			SpecHash:           r.SpecHash,
			AppliedAt:          r.AppliedAt.UTC().Format(time.RFC3339Nano),
			NormalizedSpecJSON: r.NormalizedSpecJSON,
		})
	}
	return out
}

// traceIDFromEvents returns an OTel trace id from event data when present (issue #108).
func traceIDFromEvents(events []state.TraceEvent) string {
	for _, e := range events {
		if id := parseTraceID(e.DataJSON); id != "" {
			return id
		}
	}
	return ""
}

func parseTraceID(dataJSON string) string {
	if dataJSON == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(dataJSON), &m); err != nil {
		return ""
	}
	for _, key := range []string{"trace_id", "traceId", "traceID"} {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}
