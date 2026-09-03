package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
)

// deploymentStoreTx implements [state.DeploymentTxStore] using a single SQL transaction, so a plan
// application and the deployment snapshot it produces commit together (issue #387).
type deploymentStoreTx struct {
	tx *sql.Tx
}

// Compile-time check: the transactional store exposes both the applied-row and the snapshot/artifact
// mutations so apply can persist them in one transaction.
var _ state.DeploymentTxStore = (*deploymentStoreTx)(nil)

func (t *deploymentStoreTx) UpsertAppliedResource(ctx context.Context, r state.AppliedResource) error {
	return upsertAppliedResource(ctx, t.tx, r)
}

func (t *deploymentStoreTx) GetAppliedResource(ctx context.Context, env string, id spec.ResourceID) (*state.AppliedResource, error) {
	return getAppliedResource(ctx, t.tx, env, id)
}

func (t *deploymentStoreTx) ListAppliedResourcesByEnv(ctx context.Context, env string) ([]state.AppliedResource, error) {
	return listAppliedResourcesByEnv(ctx, t.tx, env)
}

func (t *deploymentStoreTx) DeleteAppliedResource(ctx context.Context, env string, id spec.ResourceID) error {
	return deleteAppliedResource(ctx, t.tx, env, id)
}

func (t *deploymentStoreTx) UpsertAppliedProject(ctx context.Context, p state.AppliedProject) error {
	return upsertAppliedProject(ctx, t.tx, p)
}

func (t *deploymentStoreTx) GetAppliedProject(ctx context.Context, env, projectName string) (*state.AppliedProject, error) {
	return getAppliedProject(ctx, t.tx, env, projectName)
}

func (t *deploymentStoreTx) PutArtifact(ctx context.Context, a state.DeploymentArtifact) error {
	return putArtifact(ctx, t.tx, a)
}

func (t *deploymentStoreTx) GetArtifact(ctx context.Context, digest string) (*state.DeploymentArtifact, error) {
	return getArtifact(ctx, t.tx, digest)
}

func (t *deploymentStoreTx) PutSnapshot(ctx context.Context, s state.DeploymentSnapshot) error {
	return putSnapshot(ctx, t.tx, s)
}

func (t *deploymentStoreTx) GetSnapshot(ctx context.Context, digest string) (*state.DeploymentSnapshot, error) {
	return getSnapshot(ctx, t.tx, digest)
}

func (t *deploymentStoreTx) SetCurrentSnapshot(ctx context.Context, env, digest string) error {
	return setCurrentSnapshot(ctx, t.tx, env, digest, time.Now())
}

func (t *deploymentStoreTx) CurrentSnapshotDigestForEnv(ctx context.Context, env string) (string, error) {
	return currentSnapshotDigestForEnv(ctx, t.tx, env)
}

func (t *deploymentStoreTx) PruneUnreferencedArtifacts(ctx context.Context) (int64, error) {
	return pruneUnreferencedArtifacts(ctx, t.tx)
}

// RunDeploymentTx runs fn with a [state.DeploymentTxStore] backed by one SQLite transaction, so the
// applied rows and the deployment snapshot fn persists commit together. The transaction commits only
// if fn returns nil.
func (s *Store) RunDeploymentTx(ctx context.Context, fn func(ctx context.Context, dep state.DeploymentTxStore) error) error {
	if s == nil || s.db == nil {
		return errors.New("sqlite: nil store")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	w := &deploymentStoreTx{tx: tx}
	if err := fn(ctx, w); err != nil {
		return err
	}
	return tx.Commit()
}
