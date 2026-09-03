package state

import (
	"context"
	"time"

	"github.com/Terfyn/terfyn/internal/spec"
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

// ArtifactStore persists immutable, content-addressed deployment artifacts and snapshots
// (design doc §14, issue #207). Writes are insert-if-absent (dedupe by content); artifacts and
// snapshots are never mutated once written.
type ArtifactStore interface {
	// PutArtifact stores a immutable payload, deduped by Digest. Re-putting an identical digest is
	// a no-op and never overwrites the stored payload.
	PutArtifact(ctx context.Context, a DeploymentArtifact) error
	// GetArtifact returns the artifact for digest, or sql.ErrNoRows.
	GetArtifact(ctx context.Context, digest string) (*DeploymentArtifact, error)
	// PutSnapshot stores a snapshot row, deduped by Digest. Re-putting is a no-op.
	PutSnapshot(ctx context.Context, s DeploymentSnapshot) error
	// GetSnapshot returns the snapshot for digest, or sql.ErrNoRows.
	GetSnapshot(ctx context.Context, digest string) (*DeploymentSnapshot, error)
	// SetCurrentSnapshot points env at digest — the snapshot deployed now. Called on every apply,
	// including a re-apply of an earlier digest (rollback), so the pointer always reflects the last
	// apply, not first-insert order.
	SetCurrentSnapshot(ctx context.Context, env, digest string) error
	// CurrentSnapshotDigestForEnv returns the snapshot digest currently deployed for env (the apply
	// pointer), or sql.ErrNoRows. Used to flag a run as executing a superseded artifact
	// (inspect/logs): superseded == run's pinned digest differs from this.
	CurrentSnapshotDigestForEnv(ctx context.Context, env string) (string, error)
	// PruneUnreferencedArtifacts deletes snapshots not referenced by any run and artifacts not
	// referenced by any surviving snapshot. Trace pruning must not orphan an artifact a run still
	// references, so this is reference-guarded. Returns rows removed.
	PruneUnreferencedArtifacts(ctx context.Context) (removed int64, err error)
}

// DeploymentTxStore exposes every deployment-side mutation available inside a single apply
// transaction: the applied_* rows ([DeploymentStore]) and the content-addressed snapshot/artifact
// writes plus the env→snapshot pointer ([ArtifactStore]). Apply persists the plan rows and the
// deployment snapshot through this combined store so both commit atomically (issue #387); a failure
// in either half rolls back the whole apply instead of leaving applied_resources ahead of the
// current-snapshot pointer.
type DeploymentTxStore interface {
	DeploymentStore
	ArtifactStore
}

// TransactionalDeployment runs deployment mutations in a single atomic transaction when supported
// (design doc §12.2 D apply, issue #15). fn receives a [DeploymentTxStore] so the applied rows and
// the deployment snapshot it persists share one transaction (issue #387).
type TransactionalDeployment interface {
	RunDeploymentTx(ctx context.Context, fn func(ctx context.Context, dep DeploymentTxStore) error) error
}

// RuntimeStore persists execution rows from design doc §14.2 (runs, run_steps, trace_events).
//
// Thread-safety: same expectations as [DeploymentStore].
type RuntimeStore interface {
	StartRun(ctx context.Context, r Run) error
	FinishRun(ctx context.Context, runID, status string, finishedAt time.Time, outputJSON, errorText string, totalCostUSD float64) error
	UpsertRunStep(ctx context.Context, st RunStep) error
	AppendTraceEvent(ctx context.Context, runID string, ts time.Time, eventType, actorType, stepID, dataJSON string) (seq int64, err error)
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
