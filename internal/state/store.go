package state

import (
	"context"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// DeploymentStore persists deployment rows from design doc §14.1 (applied_resources, applied_projects).
//
// Thread-safety: MVP targets a single-process CLI. Implementations are not required to support
// arbitrary concurrent callers; treat the store as non-thread-safe unless a backend documents otherwise.
type DeploymentStore interface {
	UpsertAppliedResource(ctx context.Context, r AppliedResource) error
	GetAppliedResource(ctx context.Context, env string, id spec.ResourceID) (*AppliedResource, error)
	ListAppliedResourcesByEnv(ctx context.Context, env string) ([]AppliedResource, error)
	UpsertAppliedProject(ctx context.Context, p AppliedProject) error
	GetAppliedProject(ctx context.Context, env, projectName string) (*AppliedProject, error)
}

// RuntimeStore persists execution rows from design doc §14.2 (runs, run_steps, trace_events).
//
// Thread-safety: same expectations as [DeploymentStore].
type RuntimeStore interface {
	StartRun(ctx context.Context, r Run) error
	FinishRun(ctx context.Context, runID, status string, finishedAt time.Time, outputJSON, errorText string, totalCostUSD float64) error
	UpsertRunStep(ctx context.Context, st RunStep) error
	AppendTraceEvent(ctx context.Context, runID string, ts time.Time, eventType string, stepID string, dataJSON string) (seq int64, err error)
	GetRun(ctx context.Context, runID string) (*Run, error)
	ListTraceEventsByRunID(ctx context.Context, runID string) ([]TraceEvent, error)
}
