package apply

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/deploy"
	"github.com/Terfyn/terfyn/internal/plan"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
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
	p, err := pl.ComputePlan(ctx, "dev", g, nil)
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
	p1, err := pl.ComputePlan(ctx, "dev", gFull, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ap.ApplyPlan(ctx, "dev", gFull, p1, t0); err != nil {
		t.Fatal(err)
	}

	gOnly := minimalGraph()
	p2, err := pl.ComputePlan(ctx, "dev", gOnly, nil)
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

// TestApplyPlanAndFinalize_rollsBackPlanRowsWhenFinalizeFails is the regression for issue #387: the
// applied_* rows and the deployment snapshot commit in one transaction, so a failure while
// persisting the snapshot must leave no applied rows behind (never applied_resources ahead of a
// stale current-snapshot pointer).
func TestApplyPlanAndFinalize_rollsBackPlanRowsWhenFinalizeFails(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "apply-finalize-fail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	g := graphWithAgent()
	p, err := plan.NewPlanner(st).ComputePlan(ctx, "dev", g, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Operations) == 0 {
		t.Fatal("want non-empty plan")
	}

	boom := errors.New("snapshot persistence failed")
	at := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	err = NewApplier(st).ApplyPlanAndFinalize(ctx, "dev", g, p, at, func(ctx context.Context, store state.DeploymentTxStore) error {
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("want finalize error, got %v", err)
	}

	// The whole apply must have rolled back: no applied rows, no current-snapshot pointer.
	list, err := st.ListAppliedResourcesByEnv(ctx, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("plan rows must roll back with the failed snapshot, got %+v", list)
	}
	if _, err := st.CurrentSnapshotDigestForEnv(ctx, "dev"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("current-snapshot pointer must not be set, got %v", err)
	}
}

// TestApplyPlanAndFinalize_commitsRowsAndSnapshotTogether verifies the success path: applied rows,
// the snapshot, and the env→snapshot pointer are all visible after one atomic apply (issue #387).
func TestApplyPlanAndFinalize_commitsRowsAndSnapshotTogether(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "apply-finalize-ok.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	g := graphWithAgent()
	p, err := plan.NewPlanner(st).ComputePlan(ctx, "dev", g, nil)
	if err != nil {
		t.Fatal(err)
	}
	built, _, err := deploy.Prepare(g, "dev", "v1", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	err = NewApplier(st).ApplyPlanAndFinalize(ctx, "dev", g, p, at, func(ctx context.Context, store state.DeploymentTxStore) error {
		if _, err := deploy.Persist(ctx, store, built); err != nil {
			return err
		}
		return store.SetCurrentSnapshot(ctx, "dev", built.Snapshot.Digest)
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.GetAppliedResource(ctx, "dev", spec.ResourceID{Kind: spec.KindAgent, Name: "rev"}); err != nil {
		t.Fatalf("agent row should be committed: %v", err)
	}
	digest, err := st.CurrentSnapshotDigestForEnv(ctx, "dev")
	if err != nil {
		t.Fatalf("current snapshot should be set: %v", err)
	}
	if digest != built.Snapshot.Digest {
		t.Fatalf("current snapshot digest = %q, want %q", digest, built.Snapshot.Digest)
	}
	if _, err := st.GetSnapshot(ctx, built.Snapshot.Digest); err != nil {
		t.Fatalf("snapshot row should be committed: %v", err)
	}
}

func TestApplyPlan_rejectsStaleDeploymentBaseline(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "apply-stale.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	pl := plan.NewPlanner(st)
	ap := NewApplier(st)
	t0 := time.Date(2026, 4, 11, 10, 0, 0, 0, time.UTC)

	gFull := graphWithAgent()
	pCreate, err := pl.ComputePlan(ctx, "dev", gFull, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pCreate.Operations) == 0 || pCreate.DeploymentBaseline == "" {
		t.Fatalf("want non-empty plan with baseline, got ops=%d baseline=%q", len(pCreate.Operations), pCreate.DeploymentBaseline)
	}
	if err := ap.ApplyPlan(ctx, "dev", gFull, pCreate, t0); err != nil {
		t.Fatal(err)
	}

	gOnly := minimalGraph()
	pDelete, err := pl.ComputePlan(ctx, "dev", gOnly, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pDelete.DeploymentBaseline == "" {
		t.Fatal("missing baseline")
	}

	// Simulate another writer: deployment no longer matches the fingerprint embedded in pDelete.
	if err := st.DeleteAppliedResource(ctx, "dev", spec.ResourceID{Kind: spec.KindAgent, Name: "rev"}); err != nil {
		t.Fatal(err)
	}

	err = ap.ApplyPlan(ctx, "dev", gOnly, pDelete, t0.Add(time.Hour))
	if !errors.Is(err, ErrDeploymentStateChanged) {
		t.Fatalf("want ErrDeploymentStateChanged, got %v", err)
	}
}
