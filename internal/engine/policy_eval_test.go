package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/spec"
)

func TestCompiledWorkflowEvaluator_unknownPolicyErrors(t *testing.T) {
	g := &spec.ProjectGraph{
		Workflows: map[string]*spec.WorkflowResource{
			"demo": {
				Metadata: spec.Metadata{Name: "demo"},
				Spec:     spec.WorkflowSpec{Policy: "missing"},
			},
		},
	}
	_, err := compiledWorkflowEvaluator("", g, "missing", false)
	if err == nil {
		t.Fatal("expected error for unknown policy")
	}
	if !strings.Contains(err.Error(), "compile policy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCompiledWorkflowEvaluator_noPolicyFallsBackToSafetyDerived is issue #438: a project that
// declares no policy resolves its workflows to the implicit "default" name; with no such policy the
// run must fall back to the safety-derived evaluator (closed-world manifest still enforced, tool
// safety metadata gates approvals — semantics tested in internal/policy) instead of failing "unknown
// policy default". The fallback applies whether the run is fresh or pinned.
func TestCompiledWorkflowEvaluator_noPolicyFallsBackToSafetyDerived(t *testing.T) {
	g := &spec.ProjectGraph{
		Workflows: map[string]*spec.WorkflowResource{
			"demo": {Metadata: spec.Metadata{Name: "demo"}, Spec: spec.WorkflowSpec{}},
		},
		// A side-effecting (untrusted) tool and a trusted one — no operations block, so the manifest is
		// open and a call reaches the safety-derived decision. No policies, no defaults.policy → the
		// implicit "default" is undeclared.
		Tools: map[string]*spec.ToolResource{
			"slack": {Metadata: spec.Metadata{Name: "slack"}, Spec: spec.ToolSpec{Type: "mock",
				Safety: &spec.ToolSafety{Trusted: spec.BoolPtr(false), SideEffects: spec.BoolPtr(true)}}},
			"docs": {Metadata: spec.Metadata{Name: "docs"}, Spec: spec.ToolSpec{Type: "mock",
				Safety: &spec.ToolSafety{Trusted: spec.BoolPtr(true)}}},
		},
	}
	for _, pinned := range []bool{false, true} {
		ev, err := compiledWorkflowEvaluator("", g, "", pinned)
		if err != nil {
			t.Fatalf("pinned=%v: a no-policy project must resolve to the safety-derived evaluator, got %v", pinned, err)
		}
		if ev == nil {
			t.Fatalf("pinned=%v: expected a non-nil evaluator", pinned)
		}
		// Assert the actual authority, not just non-nil: the fallback is the safety-derived evaluator,
		// so a side-effecting/untrusted tool requires approval and a trusted tool is allowed — a future
		// refactor that swapped in a wider (e.g. permissive) evaluator would fail here.
		err = ev.CheckToolCall(context.Background(), policy.ToolCallContext{Uses: "tool.slack.message.send"})
		if d, ok := policy.AsDenied(err); !ok || d.Reason != policy.ReasonApprovalRequired {
			t.Fatalf("pinned=%v: side-effecting tool must require approval under the no-policy fallback, got %v", pinned, err)
		}
		if err := ev.CheckToolCall(context.Background(), policy.ToolCallContext{Uses: "tool.docs.read"}); err != nil {
			t.Fatalf("pinned=%v: a trusted tool must be allowed under the no-policy fallback, got %v", pinned, err)
		}
	}

	// An explicitly-named policy that does not exist still fails loudly (not "default").
	if _, err := compiledWorkflowEvaluator("", g, "missing", false); err == nil {
		t.Fatal("an explicitly-named missing policy must still error")
	}
}

func TestCompiledWorkflowEvaluator_compilesWithoutSnapshot(t *testing.T) {
	g := demoWorkflowGraph(t)
	ev, err := compiledWorkflowEvaluator("", g, "default", false)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected evaluator")
	}
}
