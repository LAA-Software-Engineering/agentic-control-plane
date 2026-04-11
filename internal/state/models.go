package state

import "time"

// AppliedResource is one row in applied_resources (design doc §14.1).
type AppliedResource struct {
	Kind               string
	Name               string
	Env                string
	SpecHash           string
	NormalizedSpecJSON string
	AppliedAt          time.Time
}

// AppliedProject is one row in applied_projects (design doc §14.1).
type AppliedProject struct {
	ProjectName string
	Env         string
	Version     string
	AppliedAt   time.Time
}

// Run is one workflow execution row in runs (design doc §14.2).
type Run struct {
	RunID        string
	WorkflowName string
	Env          string
	Status       string
	StartedAt    time.Time
	FinishedAt   *time.Time
	InputJSON    string
	OutputJSON   string
	ErrorText    string
	TotalCostUSD float64
}

// RunStep is one row in run_steps (design doc §14.2).
type RunStep struct {
	RunID      string
	StepID     string
	Status     string
	StartedAt  *time.Time
	FinishedAt *time.Time
	InputJSON  string
	OutputJSON string
	ErrorText  string
	CostUSD    float64
}

// TraceEvent is one append-only row in trace_events (design doc §14.2).
type TraceEvent struct {
	RunID     string
	Seq       int64
	Timestamp time.Time
	Type      string
	StepID    string
	DataJSON  string
}
