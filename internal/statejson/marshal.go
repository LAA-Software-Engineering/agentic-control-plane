package statejson

import (
	"encoding/json"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

// TraceEvents maps trace rows to API/CLI JSON records.
func TraceEvents(events []state.TraceEvent) []TraceEventRecord {
	out := make([]TraceEventRecord, 0, len(events))
	for _, e := range events {
		rec := TraceEventRecord{
			Seq:       e.Seq,
			Timestamp: e.Timestamp.UTC().Format(time.RFC3339Nano),
			Type:      trace.NormalizeStoredEventType(e.Type),
			ActorType: e.ActorType,
			StepID:    e.StepID,
			TenantID:  e.TenantID,
			ThreadID:  e.ThreadID,
			ActorID:   e.ActorID,
		}
		if et, known := trace.ParseEventType(rec.Type); known {
			rec.TimelineIcon = et.TimelineIcon()
			rec.TimelineGroup = et.TimelineGroup()
			rec.SpanName = et.SpanName()
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

// Run maps one runs row to a JSON record.
func Run(r state.Run) RunRecord {
	in := r.InputJSON
	if in == "" {
		in = "{}"
	}
	rec := RunRecord{
		RunID:          r.RunID,
		Workflow:       r.WorkflowName,
		Env:            r.Env,
		Status:         r.Status,
		StartedAt:      r.StartedAt.UTC().Format(time.RFC3339Nano),
		TotalCostUsd:   r.TotalCostUSD,
		TenantID:       r.TenantID,
		ThreadID:       r.ThreadID,
		ActorID:        r.ActorID,
		ParentRunID:    r.ParentRunID,
		RequestID:      r.RequestID,
		IdempotencyKey: r.IdempotencyKey,
		Source:         r.Source,
		Input:          json.RawMessage(in),
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

// Runs maps run rows to JSON records.
func Runs(runs []state.Run) []RunRecord {
	out := make([]RunRecord, 0, len(runs))
	for _, r := range runs {
		out = append(out, Run(r))
	}
	return out
}

// AppliedResource maps one applied_resources row.
func AppliedResource(r state.AppliedResource) AppliedResourceRecord {
	return AppliedResourceRecord{
		Kind:               r.Kind,
		Name:               r.Name,
		Env:                r.Env,
		SpecHash:           r.SpecHash,
		AppliedAt:          r.AppliedAt.UTC().Format(time.RFC3339Nano),
		NormalizedSpecJSON: r.NormalizedSpecJSON,
	}
}

// AppliedResources maps applied resource rows.
func AppliedResources(rows []state.AppliedResource) []AppliedResourceRecord {
	out := make([]AppliedResourceRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, AppliedResource(r))
	}
	return out
}

// AppliedProject maps one applied_projects row.
func AppliedProject(p *state.AppliedProject) *AppliedProjectRecord {
	if p == nil {
		return nil
	}
	return &AppliedProjectRecord{
		ProjectName: p.ProjectName,
		Env:         p.Env,
		Version:     p.Version,
		AppliedAt:   p.AppliedAt.UTC().Format(time.RFC3339Nano),
	}
}
