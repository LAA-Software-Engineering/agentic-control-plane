package plan_test

import (
	"context"
	"errors"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// stubDeploy is a minimal [state.DeploymentStore] so this package never imports sqlite.
type stubDeploy struct {
	list []state.AppliedResource
}

func (s *stubDeploy) UpsertAppliedResource(context.Context, state.AppliedResource) error {
	return errors.New("stub")
}

func (s *stubDeploy) GetAppliedResource(context.Context, string, spec.ResourceID) (*state.AppliedResource, error) {
	return nil, errors.New("stub")
}

func (s *stubDeploy) ListAppliedResourcesByEnv(context.Context, string) ([]state.AppliedResource, error) {
	return s.list, nil
}

func (s *stubDeploy) UpsertAppliedProject(context.Context, state.AppliedProject) error {
	return errors.New("stub")
}

func (s *stubDeploy) GetAppliedProject(context.Context, string, string) (*state.AppliedProject, error) {
	return nil, errors.New("stub")
}

func TestPlanner_listAppliedResources_usesDeploymentStoreOnly(t *testing.T) {
	st := &stubDeploy{list: []state.AppliedResource{{Name: "agent-a", Env: "dev", Kind: "Agent"}}}
	p := plan.NewPlanner(st)
	got, err := p.ListAppliedResources(context.Background(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "agent-a" {
		t.Fatalf("got %+v", got)
	}
}

var _ state.DeploymentStore = (*stubDeploy)(nil)
