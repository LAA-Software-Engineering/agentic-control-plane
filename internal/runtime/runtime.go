package runtime

import (
	"context"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

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
	// Resume continues an existing run from its latest checkpoint (issue #105).
	// RunID must be set; InputJSON and WorkflowName are loaded from the persisted run.
	Resume bool
	// AutoApprove skips interactive HITL prompts and approves gated tool calls (issue #106).
	AutoApprove bool
	// HitlActor attributes approval decisions in trace events (default: operator or $USER).
	HitlActor string
	// HitlDecision supplies an explicit decision when resuming an interrupted run (issue #106).
	HitlDecision *HitlDecisionOptions
}

// HitlDecisionOptions configures a non-interactive HITL resolution on resume.
type HitlDecisionOptions struct {
	Kind         spec.HitlDecisionKind
	EditedWith   map[string]any
	SwitchTarget string
}

// WorkflowRunner loads declarative state and executes a workflow locally (design doc section 16 MVP).
type WorkflowRunner interface {
	ExecuteWorkflow(ctx context.Context, opts WorkflowRunOptions) (runID string, err error)
}
