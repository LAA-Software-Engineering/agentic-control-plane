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
	DeleteAppliedResource(ctx context.Context, env string, id spec.ResourceID) error
	UpsertAppliedProject(ctx context.Context, p AppliedProject) error
	GetAppliedProject(ctx context.Context, env, projectName string) (*AppliedProject, error)
}

// TransactionalDeployment runs deployment mutations in a single atomic transaction when supported
// (design doc §12.2 D apply, issue #15).
type TransactionalDeployment interface {
	RunDeploymentTx(ctx context.Context, fn func(ctx context.Context, dep DeploymentStore) error) error
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
	// ListRecentRuns returns runs ordered by started_at descending (newest first), limited to limit rows.
	ListRecentRuns(ctx context.Context, limit int) ([]Run, error)
	// ListRunsByWorkflow returns runs for the given workflow_name ordered by started_at descending.
	ListRunsByWorkflow(ctx context.Context, workflowName string, limit int) ([]Run, error)
	// ListRunsFiltered returns runs matching optional tenant/thread/actor/workflow filters.
	ListRunsFiltered(ctx context.Context, filter RunListFilter) ([]Run, error)
	ListTraceEventsByRunID(ctx context.Context, runID string) ([]TraceEvent, error)
	// DeleteRunsStartedBefore removes every run with started_at strictly before cutoff (UTC), and
	// associated run_steps / trace_events (SQLite: ON DELETE CASCADE). Used for trace retention (issue #75).
	DeleteRunsStartedBefore(ctx context.Context, cutoff time.Time) (deleted int64, err error)
	// SaveCheckpoint appends a checkpoint row for run_id (monotonic seq per run).
	SaveCheckpoint(ctx context.Context, cp RunCheckpoint) error
	// GetLatestCheckpoint returns the newest checkpoint for run_id or sql.ErrNoRows.
	GetLatestCheckpoint(ctx context.Context, runID string) (*RunCheckpoint, error)
	// UpdateRunStatus sets runs.status without finishing the run (issue #105 interrupted).
	UpdateRunStatus(ctx context.Context, runID, status string) error
}
