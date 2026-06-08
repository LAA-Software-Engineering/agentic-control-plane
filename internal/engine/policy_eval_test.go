package engine

import (
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
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
	_, err := compiledWorkflowEvaluator("", g, "missing")
	if err == nil {
		t.Fatal("expected error for unknown policy")
	}
	if !strings.Contains(err.Error(), "compile policy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompiledWorkflowEvaluator_compilesWithoutSnapshot(t *testing.T) {
	g := demoWorkflowGraph(t)
	ev, err := compiledWorkflowEvaluator("", g, "default")
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected evaluator")
	}
}
