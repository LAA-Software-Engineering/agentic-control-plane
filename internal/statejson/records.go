package statejson

import "encoding/json"

// TraceEventRecord is one trace_events row (agentctl logs -o json, inspector /api/runs/{id}).
type TraceEventRecord struct {
	Seq           int64           `json:"seq"`
	Timestamp     string          `json:"timestamp"`
	Type          string          `json:"type"`
	ActorType     string          `json:"actorType,omitempty"`
	StepID        string          `json:"stepId,omitempty"`
	TenantID      string          `json:"tenantId,omitempty"`
	ThreadID      string          `json:"threadId,omitempty"`
	ActorID       string          `json:"actorId,omitempty"`
	TimelineGroup string          `json:"timelineGroup,omitempty"`
	TimelineIcon  string          `json:"timelineIcon,omitempty"`
	SpanName      string          `json:"spanName,omitempty"`
	Data          json.RawMessage `json:"data"`
}

// RunRecord is one runs row (agentctl logs -o json, inspector /api/runs).
type RunRecord struct {
	RunID          string          `json:"runId"`
	Workflow       string          `json:"workflow"`
	Env            string          `json:"env"`
	Status         string          `json:"status"`
	StartedAt      string          `json:"startedAt"`
	FinishedAt     string          `json:"finishedAt,omitempty"`
	TotalCostUsd   float64         `json:"totalCostUsd"`
	TenantID       string          `json:"tenantId,omitempty"`
	ThreadID       string          `json:"threadId,omitempty"`
	ActorID        string          `json:"actorId,omitempty"`
	ParentRunID    string          `json:"parentRunId,omitempty"`
	RequestID      string          `json:"requestId,omitempty"`
	IdempotencyKey string          `json:"idempotencyKey,omitempty"`
	Source         string          `json:"source,omitempty"`
	Input          json.RawMessage `json:"input"`
	Output         json.RawMessage `json:"output,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// AppliedResourceRecord is one applied_resources row (agentctl state list -o json).
type AppliedResourceRecord struct {
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	Env                string `json:"env"`
	SpecHash           string `json:"specHash"`
	AppliedAt          string `json:"appliedAt"`
	NormalizedSpecJSON string `json:"normalizedSpecJson"`
}

// AppliedProjectRecord is one applied_projects row when present in state list JSON.
type AppliedProjectRecord struct {
	ProjectName string `json:"projectName"`
	Env         string `json:"env"`
	Version     string `json:"version"`
	AppliedAt   string `json:"appliedAt"`
}

// RunListPayload is the JSON envelope for agentctl logs (no filters).
type RunListPayload struct {
	StatePath string      `json:"statePath"`
	Runs      []RunRecord `json:"runs"`
}

// RunEventsPayload is the JSON envelope for agentctl logs --run.
type RunEventsPayload struct {
	StatePath string             `json:"statePath"`
	RunID     string             `json:"runId"`
	Workflow  string             `json:"workflow,omitempty"`
	Events    []TraceEventRecord `json:"events"`
}

// StateListPayload is the JSON envelope for agentctl state list -o json.
type StateListPayload struct {
	Environment    string                  `json:"environment"`
	StatePath      string                  `json:"statePath"`
	Resources      []AppliedResourceRecord `json:"resources"`
	AppliedProject *AppliedProjectRecord   `json:"appliedProject"`
}
