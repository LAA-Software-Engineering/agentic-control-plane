package plan

import (
	"context"
	"errors"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// Planner reads deployment state to compare desired vs applied resources (design doc §5.2).
type Planner struct {
	Deploy state.DeploymentStore
}

// NewPlanner returns a planner backed by dep. dep must not be nil when methods are called.
func NewPlanner(dep state.DeploymentStore) *Planner {
	return &Planner{Deploy: dep}
}

// ListAppliedResources returns applied resources for env (MVP entry point for plan input).
func (p *Planner) ListAppliedResources(ctx context.Context, env string) ([]state.AppliedResource, error) {
	if p == nil || p.Deploy == nil {
		return nil, errors.New("plan: nil deployment store")
	}
	return p.Deploy.ListAppliedResourcesByEnv(ctx, env)
}
