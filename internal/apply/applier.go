package apply

import (
	"context"
	"errors"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
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

	run := func(ctx context.Context, dep state.DeploymentStore) error {
		if err := assertDeploymentBaseline(ctx, dep, env, projectName, p); err != nil {
			return err
		}
		return executePlan(ctx, dep, env, p, at, projectName, projectVersion)
	}
	if tx, ok := a.Deploy.(state.TransactionalDeployment); ok {
		return tx.RunDeploymentTx(ctx, run)
	}
	return run(ctx, a.Deploy)
}
