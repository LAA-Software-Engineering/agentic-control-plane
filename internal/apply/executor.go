package apply

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

func executePlan(
	ctx context.Context,
	dep state.DeploymentStore,
	env string,
	p *plan.Plan,
	at time.Time,
	projectName, projectVersion string,
) error {
	for _, op := range p.Operations {
		switch op.Action {
		case plan.ActionCreate, plan.ActionUpdate:
			if strings.TrimSpace(op.SpecHash) == "" || strings.TrimSpace(op.NormalizedSpecJSON) == "" {
				return fmt.Errorf("apply: %s operation for %s missing spec hash or normalized JSON", op.Action, op.Target.String())
			}
			if err := dep.UpsertAppliedResource(ctx, state.AppliedResource{
				Kind:               op.Target.Kind,
				Name:               op.Target.Name,
				Env:                env,
				SpecHash:           op.SpecHash,
				NormalizedSpecJSON: op.NormalizedSpecJSON,
				AppliedAt:          at,
			}); err != nil {
				return err
			}
		case plan.ActionDelete:
			if err := dep.DeleteAppliedResource(ctx, env, op.Target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("apply: unknown operation action %q", op.Action)
		}
	}
	return dep.UpsertAppliedProject(ctx, state.AppliedProject{
		ProjectName: projectName,
		Env:         env,
		Version:     projectVersion,
		AppliedAt:   at,
	})
}
