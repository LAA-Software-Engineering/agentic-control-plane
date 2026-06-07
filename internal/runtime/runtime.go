package runtime

import (
	"context"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// HealthState reports runtime readiness.
type HealthState string

const (
	// HealthOK means the runtime is ready to accept work.
	HealthOK HealthState = "ok"
	// HealthDegraded means the runtime can execute but with reduced capability.
	HealthDegraded HealthState = "degraded"
	// HealthError means the runtime cannot execute workflows.
	HealthError HealthState = "error"
)

// HealthStatus is returned by [Runtime.Health].
type HealthStatus struct {
	State   HealthState
	Details string
}

// RunResult is the outcome of [Runtime.Invoke] or [Runtime.Resume].
type RunResult struct {
	RunID string
}

// Deps are control-plane supplied dependencies shared by all runtime implementations.
type Deps struct {
	Store        state.RuntimeStore
	AgentVersion string
	Now          func() time.Time
}

// InvokeOptions configures a new workflow execution.
type InvokeOptions struct {
	// RunID optional; when empty the runtime generates one.
	RunID string
	// WorkflowName is the metadata.name of a Workflow resource.
	WorkflowName string
	// Env is stored on the run row (e.g. deployment target label).
	Env string
	// EnvironmentName is the CLI -e overlay name; empty skips Environment resource overrides.
	EnvironmentName string
	// InputJSON is JSON object bytes for workflow input. Empty means {}.
	InputJSON []byte
	// ApprovedActions are full tool uses strings approved for policy gates.
	ApprovedActions []string
	// AutoApprove skips interactive HITL prompts and approves gated tool calls.
	AutoApprove bool
	// HitlActor attributes approval decisions in trace events.
	HitlActor string
	// Attribution scopes the run for multi-tenant logs and compliance.
	TenantID       string
	ThreadID       string
	ActorID        string
	ParentRunID    string
	RequestID      string
	IdempotencyKey string
	Source         string
	// RequireAttribution rejects runs when tenant_id, thread_id, or actor_id is omitted.
	RequireAttribution bool
}

// ResumeOptions continues an existing run from its latest checkpoint.
type ResumeOptions struct {
	// RunID is required.
	RunID string
	// EnvironmentName is the CLI -e value; must match the persisted run when pinned.
	EnvironmentName string
	// ApprovedActions are full tool uses strings approved for policy gates.
	ApprovedActions []string
	// AutoApprove skips interactive HITL prompts and approves gated tool calls.
	AutoApprove bool
	// HitlActor attributes approval decisions in trace events.
	HitlActor string
	// HitlDecision supplies an explicit decision when resuming an interrupted run.
	HitlDecision *HitlDecisionOptions
	// Attribution fields on resume are ignored; persisted run attribution is reused.
	TenantID string
	ThreadID string
	ActorID  string
}

// HitlDecisionOptions configures a non-interactive HITL resolution on resume.
type HitlDecisionOptions struct {
	Kind         spec.HitlDecisionKind
	EditedWith   map[string]any
	SwitchTarget string
}

// Runtime executes workflows from a resolved configuration snapshot supplied by the control plane.
// Implementations must not reload project YAML/TOML; they receive [config.ResolvedConfig] only.
type Runtime interface {
	Invoke(ctx context.Context, cfg *config.ResolvedConfig, opts InvokeOptions) (RunResult, error)
	Resume(ctx context.Context, cfg *config.ResolvedConfig, opts ResumeOptions) (RunResult, error)
	Health(ctx context.Context) HealthStatus
}

// WorkflowRunOptions configures a single workflow execution for [WorkflowRunner].
//
// Deprecated: prefer [InvokeOptions] and [ResumeOptions] on [Runtime].
type WorkflowRunOptions struct {
	RunID              string
	WorkflowName       string
	EnvironmentName    string
	Env                string
	InputJSON          []byte
	ApprovedActions    []string
	Resume             bool
	AutoApprove        bool
	HitlActor          string
	HitlDecision       *HitlDecisionOptions
	TenantID           string
	ThreadID           string
	ActorID            string
	ParentRunID        string
	RequestID          string
	IdempotencyKey     string
	Source             string
	RequireAttribution bool
}

// WorkflowRunner executes a workflow from a resolved configuration snapshot.
//
// Deprecated: prefer [Runtime].
type WorkflowRunner interface {
	ExecuteWorkflow(ctx context.Context, cfg *config.ResolvedConfig, opts WorkflowRunOptions) (runID string, err error)
}
