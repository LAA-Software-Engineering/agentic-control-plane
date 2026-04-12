package policy

import "time"

// RunContext carries execution state for policy checks (wall clock, cost, CLI approvals).
type RunContext struct {
	StartedAt time.Time
	// Elapsed is wall-clock time since run start (caller supplies).
	Elapsed time.Duration
	// AccumulatedCostUSD is total cost so far for the run.
	AccumulatedCostUSD float64
	// ApprovedActions lists full uses strings passed via repeated --approve on run.
	ApprovedActions []string
}

// StepContext is one workflow step for CheckStep.
type StepContext struct {
	StepID string
	// OutputIsStructured is true when step output is a structured object (JSON map), not opaque text.
	OutputIsStructured bool
}

// ToolCallContext is a resolved tool step before the executor runs.
type ToolCallContext struct {
	Run    RunContext
	StepID string
	Uses   string
}
