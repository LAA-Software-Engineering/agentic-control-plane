package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// deploymentStoreTx implements [state.DeploymentStore] using a single SQL transaction.
type deploymentStoreTx struct {
	tx *sql.Tx
}

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

// RunDeploymentTx runs fn with a [state.DeploymentStore] backed by one SQLite transaction.
// The transaction commits only if fn returns nil.
func (s *Store) RunDeploymentTx(ctx context.Context, fn func(ctx context.Context, dep state.DeploymentStore) error) error {
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
