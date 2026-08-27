package plan

import (
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/execir"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func sampleWorkflow() *spec.WorkflowResource {
	return &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindWorkflow,
		Metadata:   spec.Metadata{Name: "W"},
		Spec: spec.WorkflowSpec{
			Steps: []spec.WorkflowStep{{ID: "s1", Uses: "tool.github.get_pr"}},
		},
	}
}

// TestWorkflowSpecHash_ExecDigestFolds proves ADR 002 §5: two workflows with an
// identical resource projection but different execution IR get different
// spec_hashes, so a lowering change with no resource-level change still
// invalidates a stale plan.
func TestWorkflowSpecHash_ExecDigestFolds(t *testing.T) {
	t.Parallel()
	wf := sampleWorkflow()

	progA := &execir.Program{Workflow: "W", Body: []execir.Node{
		&execir.Branch{
			Cond: execir.Leaf{V: execir.Lit{V: true}},
			Then: []execir.Node{&execir.InvokeTool{Uses: "tool.github.get_pr"}},
		},
	}}
	progB := &execir.Program{Workflow: "W", Body: []execir.Node{
		&execir.Branch{
			Cond: execir.Leaf{V: execir.Lit{V: true}},
			Else: []execir.Node{&execir.InvokeTool{Uses: "tool.github.get_pr"}}, // moved to else arm
		},
	}}

	hashA, err := WorkflowSpecHashWithExec(wf, progA.Digest())
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := WorkflowSpecHashWithExec(wf, progB.Digest())
	if err != nil {
		t.Fatal(err)
	}
	if hashA == hashB {
		t.Fatalf("different execution IR must produce different spec_hash even with identical resource projection")
	}
}

// TestWorkflowSpecHash_EmptyExecUnchanged proves the fold is backwards
// compatible: with no execution IR (an empty digest), the hash equals the
// historical resource-only hash, so existing YAML deployment state and golden
// hashes are unaffected.
func TestWorkflowSpecHash_EmptyExecUnchanged(t *testing.T) {
	t.Parallel()
	wf := sampleWorkflow()
	base, err := WorkflowSpecHash(wf)
	if err != nil {
		t.Fatal(err)
	}
	folded, err := WorkflowSpecHashWithExec(wf, "")
	if err != nil {
		t.Fatal(err)
	}
	if base != folded {
		t.Fatalf("empty exec digest must not change the hash: %s vs %s", base, folded)
	}
}
