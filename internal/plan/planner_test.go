package plan

import (
	"context"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/execir"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
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

func (f *fakeDeploy) DeleteAppliedResource(context.Context, string, spec.ResourceID) error {
	return nil
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
	rows, err := desiredRows(g, nil)
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

func appliedFromDesiredExec(t *testing.T, env string, g *spec.ProjectGraph, execs map[string]*execir.Program) []state.AppliedResource {
	t.Helper()
	rows, err := desiredRows(g, execs)
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

// TestComputePlan_loweringOnlyChange_reportsUpdate is the regression for issue #377: a workflow
// whose resource projection (NormalizedSpecJSON) is byte-identical but whose execution IR changes —
// a lowering-only .agent edit such as an `if` condition, an `if`→`while` rewrite, or a `while … limit`
// bound — must be a visible plan change (its folded spec_hash moved, #260), not swallowed by the
// identical-JSON legacy-row carve-out.
func TestComputePlan_loweringOnlyChange_reportsUpdate(t *testing.T) {
	wf := &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "w"},
		Spec: spec.WorkflowSpec{Steps: []spec.WorkflowStep{{ID: "a", Uses: "tool.echo.default"}}},
	}
	g := minimalGraph()
	g.Workflows["w"] = wf

	prog1 := &execir.Program{Workflow: "w", Body: []execir.Node{&execir.InvokeTool{Bind: "a", Uses: "tool.echo.default"}}}
	prog2 := &execir.Program{Workflow: "w", Body: []execir.Node{&execir.InvokeTool{Bind: "a", Uses: "tool.echo.other"}}}
	if prog1.Digest() == prog2.Digest() {
		t.Fatal("test setup: the two programs must have different digests")
	}

	// The workflow was applied under prog1: the stored row carries prog1's folded hash and the
	// resource projection JSON.
	applied := appliedFromDesiredExec(t, "dev", g, map[string]*execir.Program{"w": prog1})

	// Re-planning with the SAME program is a no-op (the folded hashes match).
	pSame := NewPlanner(&fakeDeploy{list: applied})
	planSame, err := pSame.ComputePlan(context.Background(), "dev", g, map[string]*execir.Program{"w": prog1})
	if err != nil {
		t.Fatal(err)
	}
	if len(planSame.Operations) != 0 {
		t.Fatalf("same program must be no change, got %+v", planSame.Operations)
	}

	// A lowering-only change (prog2) — identical resource projection, different program — is 1 update.
	p := NewPlanner(&fakeDeploy{list: applied})
	plan, err := p.ComputePlan(context.Background(), "dev", g, map[string]*execir.Program{"w": prog2})
	if err != nil {
		t.Fatal(err)
	}
	wfOps := 0
	for _, op := range plan.Operations {
		if op.Target.String() != "Workflow/w" {
			t.Fatalf("unexpected op on %s: %+v", op.Target.String(), op)
		}
		if op.Action != ActionUpdate {
			t.Fatalf("want ActionUpdate for the workflow, got %+v", op)
		}
		wfOps++
	}
	if wfOps != 1 {
		t.Fatalf("want exactly 1 workflow update (issue #377), got %d: %+v", wfOps, plan.Operations)
	}
}

func TestListDesiredResourceIDs_minimalGraph(t *testing.T) {
	g := minimalGraph()
	ids, err := ListDesiredResourceIDs(g)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0].Kind != spec.KindProject || ids[0].Name != "acme" {
		t.Fatalf("%+v", ids)
	}
}

func TestComputePlan_emptyStore_allCreate(t *testing.T) {
	g := minimalGraph()
	p := NewPlanner(&fakeDeploy{list: nil})
	plan, err := p.ComputePlan(context.Background(), "dev", g, nil)
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
	plan, err := p.ComputePlan(context.Background(), "dev", g, nil)
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
	plan, err := p.ComputePlan(context.Background(), "dev", newG, nil)
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
	plan, err := p.ComputePlan(context.Background(), "dev", g, nil)
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
