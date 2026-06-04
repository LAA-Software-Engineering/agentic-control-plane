package inspect

import (
	"encoding/json"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/statejson"
)

// ListRunsResponse is GET /api/runs JSON.
type ListRunsResponse struct {
	StatePath string                `json:"statePath"`
	Workflow  string                `json:"workflow,omitempty"`
	Runs      []statejson.RunRecord `json:"runs"`
}

// RunDetailResponse is GET /api/runs/{id} JSON.
type RunDetailResponse struct {
	StatePath string                       `json:"statePath"`
	Run       statejson.RunRecord          `json:"run"`
	Steps     []StepRecord                 `json:"steps"`
	Events    []statejson.TraceEventRecord `json:"events"`
	TraceLink string                       `json:"traceLink,omitempty"`
}

// StateResponse is GET /api/state JSON.
type StateResponse struct {
	Environment    string                            `json:"environment"`
	StatePath      string                            `json:"statePath"`
	Resources      []statejson.AppliedResourceRecord `json:"resources"`
	AppliedProject *statejson.AppliedProjectRecord   `json:"appliedProject"`
}

// CheckpointsResponse is GET /api/checkpoints JSON.
type CheckpointsResponse struct {
	StatePath   string             `json:"statePath"`
	RunID       string             `json:"runId"`
	Checkpoints []CheckpointRecord `json:"checkpoints"`
}

// ErrorResponse is a stable API error body (no internal driver details on 5xx).
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// StepRecord is one run_steps row (inspector-only; not in agentctl logs JSON).
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
