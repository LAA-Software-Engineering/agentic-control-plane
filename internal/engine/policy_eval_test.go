package engine

import (
	"strings"
	"testing"

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
		// No policies declared, and no defaults.policy → the implicit "default" is undeclared.
	}
	for _, pinned := range []bool{false, true} {
		ev, err := compiledWorkflowEvaluator("", g, "", pinned)
		if err != nil {
			t.Fatalf("pinned=%v: a no-policy project must resolve to the safety-derived evaluator, got %v", pinned, err)
		}
		if ev == nil {
			t.Fatalf("pinned=%v: expected a non-nil evaluator", pinned)
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
