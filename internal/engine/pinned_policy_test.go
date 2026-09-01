package engine

import (
	"context"
	"testing"

	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/spec"
)

func toolCallGraph(permissive bool) *spec.ProjectGraph {
	approvals := &spec.PolicyApprovals{RequireAllTools: spec.BoolPtr(true)}
	if permissive {
		approvals = &spec.PolicyApprovals{Permissive: spec.BoolPtr(true)}
	}
	return &spec.ProjectGraph{
		Spec: spec.ProjectSpec{Defaults: &spec.ProjectDefaults{Policy: "default"}},
		Tools: map[string]*spec.ToolResource{
			"slack": {Metadata: spec.Metadata{Name: "slack"}, Spec: spec.ToolSpec{Type: "mock", Safety: &spec.ToolSafety{
				Trusted: spec.BoolPtr(true), SideEffects: spec.BoolPtr(false), RequiresApproval: spec.BoolPtr(false),
			}}},
		},
		Policies: map[string]*spec.PolicyResource{
			"default": {Metadata: spec.Metadata{Name: "default"}, Spec: spec.PolicySpec{Approvals: approvals}},
		},
		Workflows: map[string]*spec.WorkflowResource{
			"wf": {Metadata: spec.Metadata{Name: "wf"}, Spec: spec.WorkflowSpec{Policy: "default"}},
		},
	}
}

// The #207 invariant at the production policy boundary: a run pinned to a narrow policy must enforce
// that policy on CheckToolCall even after a widening apply wrote a permissive compiled snapshot to
// disk. The `pinned=false` half of this test is the "would fail today's disk-snapshot path" control:
// it demonstrates that without the pin the widened on-disk snapshot leaks into the run's authority.
func TestCompiledWorkflowEvaluator_pinnedIgnoresWidenedDiskSnapshot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	// Simulate a widening apply: write a permissive compiled snapshot to .agentic/policy-snapshot.json.
	widened, err := policy.CompileReferenced(toolCallGraph(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.WriteSnapshotSet(root, "digest", widened); err != nil {
		t.Fatal(err)
	}

	narrow := toolCallGraph(false) // requireAllTools: every tool call needs approval

	call := policy.ToolCallContext{Uses: "tool.slack.message.send", Run: policy.RunContext{}}

	// Pinned: authority is compiled from the hydrated (narrow) graph — the tool call is denied.
	pinned, err := compiledWorkflowEvaluator(root, narrow, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := pinned.CheckToolCall(ctx, call); err == nil {
		t.Fatal("pinned resume must enforce the narrow policy (deny), not the widened disk snapshot")
	}

	// Control (the bug this fix closes): the non-pinned path reads the widened disk snapshot and
	// ALLOWS the same call. This is exactly the leak #207 must prevent on resume.
	unpinned, err := compiledWorkflowEvaluator(root, narrow, "default", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := unpinned.CheckToolCall(ctx, call); err != nil {
		t.Fatalf("control: non-pinned path should read the widened disk snapshot and allow; got %v", err)
	}
}
