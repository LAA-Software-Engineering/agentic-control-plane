package effects

import (
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func stepWorkflow(id, callee string) spec.WorkflowStep {
	return spec.WorkflowStep{ID: id, Workflow: callee}
}

func mergeWorkflows(mps ...map[string]*spec.WorkflowResource) map[string]*spec.WorkflowResource {
	out := map[string]*spec.WorkflowResource{}
	for _, m := range mps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// TestCompute_subworkflowTransitivity proves a `workflow:` step folds the callee's effect
// bound into the caller across two nesting levels, with a witness path that shows the call
// structure (issue #194 acceptance).
func TestCompute_subworkflowTransitivity(t *testing.T) {
	t.Parallel()
	g := graph(
		toolsGithub(),
		nil,
		mergeWorkflows(
			workflow("leaf", stepUses("m", "tool.github.merge_pr")),
			workflow("mid", stepWorkflow("call_leaf", "leaf")),
			workflow("top", stepWorkflow("call_mid", "mid")),
		),
	)

	bounds := Compute(g)
	top := bounds.Workflows["top"]
	if !hasIdent(top, "destructive") {
		t.Fatalf("destructive must be transitive to top: %+v", top.Effects)
	}
	if !hasIdent(top, "github.write") {
		t.Fatalf("github.write must be transitive to top: %+v", top.Effects)
	}

	w := witnessFor(top, "destructive")
	var wfHops, toolHops int
	for _, h := range w {
		switch h.Kind {
		case KindWorkflow:
			wfHops++
		case KindToolOperation:
			toolHops++
		}
	}
	if wfHops != 3 {
		t.Fatalf("witness must nest top -> mid -> leaf (3 workflow hops): %+v", w)
	}
	if toolHops != 1 {
		t.Fatalf("witness must end at one tool operation: %+v", w)
	}
	// The nesting is ordered outermost-first.
	if w[0].Kind != KindWorkflow || w[0].Name != "top" {
		t.Fatalf("witness must start at Workflow/top: %+v", w)
	}
}

// TestCompute_subworkflowCycleTerminates guards against infinite recursion in the walker even
// when validation would reject the graph: mutual calls must still produce a finite bound.
func TestCompute_subworkflowCycleTerminates(t *testing.T) {
	t.Parallel()
	g := graph(
		toolsGithub(),
		nil,
		mergeWorkflows(
			workflow("a", stepUses("m", "tool.github.read_pr"), stepWorkflow("b", "b")),
			workflow("b", stepWorkflow("a", "a")),
		),
	)
	bounds := Compute(g) // must not hang
	if !hasIdent(bounds.Workflows["a"], "github.read") {
		t.Fatalf("a should still reach github.read: %+v", bounds.Workflows["a"].Effects)
	}
}
