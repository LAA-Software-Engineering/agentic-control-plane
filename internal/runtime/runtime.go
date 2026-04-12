package runtime

import "context"

// WorkflowRunOptions configures a single workflow execution for [WorkflowRunner].
type WorkflowRunOptions struct {
	// RunID optional; when empty the runner generates one via [util.NewRunID].
	RunID string
	// WorkflowName is the metadata.name of a Workflow resource.
	WorkflowName string
	// EnvironmentName selects an Environment resource for overrides (agents/policies). Empty skips overrides.
	EnvironmentName string
	// Env is stored on the run row (e.g. deployment target label).
	Env string
	// InputJSON is JSON object bytes for workflow input. Empty means {}.
	InputJSON []byte
	// ApprovedActions are full tool uses strings approved for policy gates.
	ApprovedActions []string
}

// WorkflowRunner loads declarative state and executes a workflow locally (design doc section 16 MVP).
type WorkflowRunner interface {
	ExecuteWorkflow(ctx context.Context, opts WorkflowRunOptions) (runID string, err error)
}
