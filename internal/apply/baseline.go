package apply

import (
	"context"
	"errors"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

func assertDeploymentBaseline(ctx context.Context, dep state.DeploymentStore, env, projectName string, p *plan.Plan) error {
	if p == nil {
		return errors.New("apply: nil plan")
	}
	want := strings.TrimSpace(p.DeploymentBaseline)
	if want == "" {
		// Plans from [plan.Planner.ComputePlan] always set a baseline; empty skips the check for tests/synthetics.
		return nil
	}
	got, err := plan.DeploymentStateFingerprint(ctx, dep, env, projectName)
	if err != nil {
		return err
	}
	if got != want {
		return ErrDeploymentStateChanged
	}
	return nil
}
