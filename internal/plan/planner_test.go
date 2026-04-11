package plan

import (
	"context"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

type fakeDeploy struct {
	list []state.AppliedResource
}

func (f *fakeDeploy) UpsertAppliedResource(context.Context, state.AppliedResource) error {
	return nil
}

func (f *fakeDeploy) GetAppliedResource(context.Context, string, spec.ResourceID) (*state.AppliedResource, error) {
	return nil, nil
}

func (f *fakeDeploy) ListAppliedResourcesByEnv(context.Context, string) ([]state.AppliedResource, error) {
	if f == nil {
		return nil, nil
	}
	return f.list, nil
}

func (f *fakeDeploy) UpsertAppliedProject(context.Context, state.AppliedProject) error { return nil }

func (f *fakeDeploy) GetAppliedProject(context.Context, string, string) (*state.AppliedProject, error) {
	return nil, nil
}

func minimalGraph() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Meta:         spec.Metadata{Name: "acme"},
		Spec:         spec.ProjectSpec{},
		Agents:       map[string]*spec.AgentResource{},
		Tools:        map[string]*spec.ToolResource{},
		Workflows:    map[string]*spec.WorkflowResource{},
		Policies:     map[string]*spec.PolicyResource{},
		Environments: map[string]*spec.EnvironmentResource{},
	}
}

func graphWithAgent(model string) *spec.ProjectGraph {
	g := minimalGraph()
	g.Agents["rev"] = &spec.AgentResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindAgent,
		Metadata:   spec.Metadata{Name: "rev"},
		Spec:       spec.AgentSpec{Model: model, Policy: "default"},
	}
	return g
}

func appliedFromDesired(t *testing.T, env string, g *spec.ProjectGraph) []state.AppliedResource {
	t.Helper()
	rows, err := desiredRows(g)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1, 0).UTC()
	var out []state.AppliedResource
	for _, r := range rows {
		out = append(out, state.AppliedResource{
			Kind:               r.id.Kind,
			Name:               r.id.Name,
			Env:                env,
			SpecHash:           r.hash,
			NormalizedSpecJSON: r.json,
			AppliedAt:          at,
		})
	}
	return out
}

func TestComputePlan_emptyStore_allCreate(t *testing.T) {
	g := minimalGraph()
	p := NewPlanner(&fakeDeploy{list: nil})
	plan, err := p.ComputePlan(context.Background(), "dev", g)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 1 {
		t.Fatalf("operations: %+v", plan.Operations)
	}
	op := plan.Operations[0]
	if op.Action != ActionCreate || op.Target.Kind != spec.KindProject || op.Target.Name != "acme" {
		t.Fatalf("got %+v", op)
	}
}

func TestComputePlan_secondPlan_noOps(t *testing.T) {
	g := minimalGraph()
	applied := appliedFromDesired(t, "dev", g)
	p := NewPlanner(&fakeDeploy{list: applied})
	plan, err := p.ComputePlan(context.Background(), "dev", g)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("want no ops, got %+v", plan.Operations)
	}
}

func TestComputePlan_changedField_updateWithDiff(t *testing.T) {
	oldG := graphWithAgent("openai/gpt-4.1")
	applied := appliedFromDesired(t, "dev", oldG)
	newG := graphWithAgent("anthropic/claude-sonnet-4")

	p := NewPlanner(&fakeDeploy{list: applied})
	plan, err := p.ComputePlan(context.Background(), "dev", newG)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 1 {
		t.Fatalf("operations: %+v", plan.Operations)
	}
	op := plan.Operations[0]
	if op.Action != ActionUpdate || op.Target.String() != "Agent/rev" {
		t.Fatalf("got %+v", op)
	}
	var sawModel bool
	for _, d := range op.Diff {
		if d.Path == "spec.model" {
			sawModel = true
			if d.Old != `"openai/gpt-4.1"` || d.New != `"anthropic/claude-sonnet-4"` {
				t.Fatalf("diff values: %#v", d)
			}
		}
	}
	if !sawModel {
		t.Fatalf("missing spec.model in %#v", op.Diff)
	}
}

func TestComputePlan_removedResource_delete(t *testing.T) {
	full := graphWithAgent("m")
	applied := appliedFromDesired(t, "dev", full)
	g := minimalGraph()

	p := NewPlanner(&fakeDeploy{list: applied})
	plan, err := p.ComputePlan(context.Background(), "dev", g)
	if err != nil {
		t.Fatal(err)
	}
	var deletes []Operation
	for _, op := range plan.Operations {
		if op.Action == ActionDelete {
			deletes = append(deletes, op)
		}
	}
	if len(deletes) != 1 {
		t.Fatalf("want 1 delete, got %+v", plan.Operations)
	}
	if deletes[0].Target.String() != "Agent/rev" {
		t.Fatalf("got %+v", deletes[0])
	}
}

var _ state.DeploymentStore = (*fakeDeploy)(nil)
