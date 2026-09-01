package local

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Terfyn/terfyn/internal/deploy"
	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/runtime"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
)

// policyGraph builds a graph whose policy CheckToolCall actually consults: requireAllTools (narrow,
// denies tool calls without approval) vs permissive (wide, allows them).
func policyGraph(permissive bool) *spec.ProjectGraph {
	approvals := &spec.PolicyApprovals{RequireAllTools: spec.BoolPtr(true)}
	if permissive {
		approvals = &spec.PolicyApprovals{Permissive: spec.BoolPtr(true)}
	}
	return &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"slack": {Metadata: spec.Metadata{Name: "slack"}, Spec: spec.ToolSpec{Type: "mock", Safety: &spec.ToolSafety{
				Trusted: spec.BoolPtr(true), SideEffects: spec.BoolPtr(false), RequiresApproval: spec.BoolPtr(false),
			}}},
		},
		Workflows: map[string]*spec.WorkflowResource{
			"wf": {Metadata: spec.Metadata{Name: "wf"}, Spec: spec.WorkflowSpec{Policy: "default"}},
		},
		Policies: map[string]*spec.PolicyResource{
			"default": {Metadata: spec.Metadata{Name: "default"}, Spec: spec.PolicySpec{Approvals: approvals}},
		},
	}
}

// The core #207 invariant, asserted at the production policy boundary (CheckToolCall): a run
// suspended under a narrow policy resumes under that narrow policy — the compiled evaluator built
// from the hydrated graph denies a tool call — even after a policy-widening apply lands a newer
// (permissive) snapshot. Resume hydrates authority from the run's pinned snapshot, never current
// config. See also engine.TestCompiledWorkflowEvaluator_pinnedIgnoresWidenedDiskSnapshot, which
// covers the on-disk policy-snapshot leak.
func TestPrepareForResume_enforcesPinnedAuthorityAfterWideningApply(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "resume.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Run starts under a narrow policy; pin its snapshot.
	narrowDigest, _, err := deploy.BuildAndPersist(ctx, st, policyGraph(false), "local", "v1", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// A later apply widens the policy (permissive) and lands a newer snapshot for the same env.
	if _, _, err := deploy.BuildAndPersist(ctx, st, policyGraph(true), "local", "v1", "", nil); err != nil {
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
	// Build the evaluator the pinned run uses (compiled from the hydrated graph) and assert it
	// enforces the narrow authority CheckToolCall actually checks.
	cp, err := policy.Compile(prep.graph, "default")
	if err != nil {
		t.Fatal(err)
	}
	ev := policy.NewCompiledEvaluator(prep.graph, cp)
	if err := ev.CheckToolCall(ctx, policy.ToolCallContext{Uses: "tool.slack.message.send"}); err == nil {
		t.Fatal("resume enforced widened authority instead of the pinned narrow policy")
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
