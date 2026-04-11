package plan

import (
	"context"
	"errors"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// Action* are [Operation.Action] values (design doc §12.2).
const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
)

// Plan is the result of comparing desired project resources to stored deployment rows (§12.2).
type Plan struct {
	Operations []Operation
	Risk       RiskSummary
}

// Operation is one create, update, or delete against a resource identity.
type Operation struct {
	Action string
	Target spec.ResourceID
	Diff   []FieldChange
}

// FieldChange is one normalized field-level delta for updates (§10.2 plan output).
type FieldChange struct {
	Path string
	Old  string
	New  string
}

// RiskSummary carries MVP plan risk signals (design doc §12.2, §10.2).
type RiskSummary struct {
	Messages []string
}

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
