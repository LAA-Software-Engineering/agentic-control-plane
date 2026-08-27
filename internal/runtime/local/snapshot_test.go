package local

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/deploy"
	"github.com/LAA-Software-Engineering/terfyn/internal/runtime"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
	"github.com/LAA-Software-Engineering/terfyn/internal/state/sqlite"
)

func policyGraph(permit []string) *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Workflows: map[string]*spec.WorkflowResource{
			"wf": {Metadata: spec.Metadata{Name: "wf"}, Spec: spec.WorkflowSpec{Policy: "default"}},
		},
		Policies: map[string]*spec.PolicyResource{
			"default": {Metadata: spec.Metadata{Name: "default"}, Spec: spec.PolicySpec{Effects: &spec.PolicyEffects{Permit: permit}}},
		},
	}
}

// The core #207 invariant: a run suspended under a narrow policy resumes under that narrow policy,
// even after a policy-widening apply lands a newer snapshot. Resume hydrates authority from the
// run's pinned snapshot, never current config.
func TestPrepareForResume_enforcesPinnedAuthorityAfterWideningApply(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "resume.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Run starts under a narrow policy; pin its snapshot.
	narrowDigest, _, err := deploy.BuildAndPersist(ctx, st, policyGraph([]string{"github.read"}), "local", "v1")
	if err != nil {
		t.Fatal(err)
	}
	// A later apply widens the policy and lands a newer snapshot for the same env.
	if _, _, err := deploy.BuildAndPersist(ctx, st, policyGraph([]string{"github.read", "github.write"}), "local", "v1"); err != nil {
		t.Fatal(err)
	}

	run := &state.Run{RunID: "r1", WorkflowName: "wf", Env: "local", Status: state.RunStatusInterrupted, DeploymentSnapshotDigest: narrowDigest}
	rt := NewRuntime(st)

	prep, pinned, err := rt.prepareForResume(ctx, run, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !pinned {
		t.Fatal("run with a pinned snapshot must resume from it, not current config")
	}
	permit := prep.graph.Policies["default"].Spec.Effects.Permit
	if len(permit) != 1 || permit[0] != "github.read" {
		t.Fatalf("resume enforced widened authority instead of the pinned narrow policy: %v", permit)
	}
}

// A run created before #207 (empty snapshot digest) falls back to the current config, unchanged.
func TestPrepareForResume_legacyRunFallsBackToCurrentConfig(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	rt := NewRuntime(st)
	cfg := testResolvedConfig(t, testRunProjRoot(t), "staging")
	run := &state.Run{RunID: "r-legacy", WorkflowName: "demo", Env: "dev"} // no snapshot digest
	prep, pinned, err := rt.prepareForResume(ctx, run, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if pinned {
		t.Fatal("legacy run without a snapshot must not report pinned")
	}
	if prep.graph == nil || prep.graph.Workflows["demo"] == nil {
		t.Fatal("fallback must resolve the current-config graph")
	}
}

// Invoke pins a deployment snapshot at run start and records its digest on the run row.
func TestInvoke_pinsDeploymentSnapshot(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "pin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	rt := NewRuntime(st)
	rc := testResolvedConfig(t, testRunProjRoot(t), "staging")
	if _, err := rt.Invoke(ctx, rc, runtime.InvokeOptions{RunID: "pinrun", WorkflowName: "demo", Env: "dev", InputJSON: []byte(`{"topic":"x"}`)}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetRun(ctx, "pinrun")
	if err != nil {
		t.Fatal(err)
	}
	if got.DeploymentSnapshotDigest == "" {
		t.Fatal("Invoke must pin a deployment snapshot digest on the run")
	}
	if _, err := st.GetSnapshot(ctx, got.DeploymentSnapshotDigest); err != nil {
		t.Fatalf("pinned snapshot must be retrievable: %v", err)
	}
}
