package apply

import (
	"context"
	"errors"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// Applier mutates deployment state from an apply operation (design doc §5.2).
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
		return errors.New("apply: nil deployment store")
	}
	return a.Deploy.UpsertAppliedResource(ctx, r)
}
