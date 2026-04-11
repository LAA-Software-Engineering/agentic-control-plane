package apply

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
)

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

func graphWithAgent() *spec.ProjectGraph {
	g := minimalGraph()
	g.Agents["rev"] = &spec.AgentResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindAgent,
		Metadata:   spec.Metadata{Name: "rev"},
		Spec:       spec.AgentSpec{Model: "m", Policy: "default"},
	}
	return g
}

func TestApplyPlan_thenListShowsResources(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "apply.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	g := minimalGraph()
	pl := plan.NewPlanner(st)
	p, err := pl.ComputePlan(ctx, "dev", g)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	ap := NewApplier(st)
	if err := ap.ApplyPlan(ctx, "dev", g, p, at); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListAppliedResourcesByEnv(ctx, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("resources: %+v", list)
	}
	if list[0].Kind != spec.KindProject || list[0].Name != "acme" {
		t.Fatalf("got %+v", list[0])
	}
	got, err := st.GetAppliedResource(ctx, "dev", spec.ResourceID{Kind: spec.KindProject, Name: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if got.SpecHash == "" || got.NormalizedSpecJSON == "" {
		t.Fatalf("missing spec material: %+v", got)
	}

	proj, err := st.GetAppliedProject(ctx, "dev", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if proj.Version == "" {
		t.Fatalf("applied_projects.version empty: %+v", proj)
	}
}

func TestApplyPlan_deleteRemovesRow(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "apply-del.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	pl := plan.NewPlanner(st)
	ap := NewApplier(st)
	t0 := time.Date(2026, 4, 11, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	gFull := graphWithAgent()
	p1, err := pl.ComputePlan(ctx, "dev", gFull)
	if err != nil {
		t.Fatal(err)
	}
	if err := ap.ApplyPlan(ctx, "dev", gFull, p1, t0); err != nil {
		t.Fatal(err)
	}

	gOnly := minimalGraph()
	p2, err := pl.ComputePlan(ctx, "dev", gOnly)
	if err != nil {
		t.Fatal(err)
	}
	if err := ap.ApplyPlan(ctx, "dev", gOnly, p2, t1); err != nil {
		t.Fatal(err)
	}

	_, err = st.GetAppliedResource(ctx, "dev", spec.ResourceID{Kind: spec.KindAgent, Name: "rev"})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("agent row should be gone: %v", err)
	}

	list, err := st.ListAppliedResourcesByEnv(ctx, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Kind != spec.KindProject {
		t.Fatalf("want project only, got %+v", list)
	}
}
