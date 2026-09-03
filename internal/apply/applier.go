package apply

import (
	"context"
	"errors"
	"time"

	"github.com/Terfyn/terfyn/internal/plan"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
)

// Applier mutates deployment state from an apply operation (design doc §5.2, §12.2 D).
type Applier struct {
	Deploy state.DeploymentStore
}

// NewApplier returns an applier backed by dep.
func NewApplier(dep state.DeploymentStore) *Applier {
	return &Applier{Deploy: dep}
}

// RecordAppliedResource upserts one applied resource row.
func (a *Applier) RecordAppliedResource(ctx context.Context, r state.AppliedResource) error {
	if a == nil || a.Deploy == nil {
		return errors.New("apply: nil applier or deployment store")
	}
	return a.Deploy.UpsertAppliedResource(ctx, r)
}

// ApplyPlan persists all plan operations and updates applied_projects for env (issue #15).
// When dep implements [state.TransactionalDeployment] (e.g. SQLite), the whole apply runs in one transaction.
func (a *Applier) ApplyPlan(ctx context.Context, env string, g *spec.ProjectGraph, p *plan.Plan, at time.Time) error {
	return a.ApplyPlanAndFinalize(ctx, env, g, p, at, nil)
}

// ApplyPlanAndFinalize applies p and then, in the same transaction, runs finalize before commit.
// finalize (nil to skip) persists the deployment snapshot and the env→snapshot pointer through the
// transaction-backed store, so the applied_* rows and the snapshot commit or roll back together
// (issue #387): a failure while writing the snapshot no longer leaves applied_resources ahead of a
// stale current-snapshot pointer. When dep is not transactional the rows and finalize still run
// sequentially on the same store.
func (a *Applier) ApplyPlanAndFinalize(ctx context.Context, env string, g *spec.ProjectGraph, p *plan.Plan, at time.Time, finalize func(ctx context.Context, store state.DeploymentTxStore) error) error {
	if a == nil || a.Deploy == nil {
		return errors.New("apply: nil applier or deployment store")
	}
	if g == nil {
		return errors.New("apply: nil project graph")
	}
	if p == nil {
		return errors.New("apply: nil plan")
	}
	if env == "" {
		return errors.New("apply: empty env")
	}
	projectName, projectVersion, err := plan.ProjectDeploymentMeta(g)
	if err != nil {
		return err
	}
	at = at.UTC()

	applyRows := func(ctx context.Context, dep state.DeploymentStore) error {
		if err := assertDeploymentBaseline(ctx, dep, env, projectName, p); err != nil {
			return err
		}
		return executePlan(ctx, dep, env, p, at, projectName, projectVersion)
	}
	if tx, ok := a.Deploy.(state.TransactionalDeployment); ok {
		return tx.RunDeploymentTx(ctx, func(ctx context.Context, dep state.DeploymentTxStore) error {
			if err := applyRows(ctx, dep); err != nil {
				return err
			}
			if finalize != nil {
				return finalize(ctx, dep)
			}
			return nil
		})
	}
	// Non-transactional fallback (e.g. a store that is only a DeploymentStore).
	if err := applyRows(ctx, a.Deploy); err != nil {
		return err
	}
	if finalize == nil {
		return nil
	}
	store, ok := a.Deploy.(state.DeploymentTxStore)
	if !ok {
		return errors.New("apply: deployment store does not support snapshot persistence")
	}
	return finalize(ctx, store)
}
