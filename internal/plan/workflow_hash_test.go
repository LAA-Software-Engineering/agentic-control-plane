package plan

import (
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestWorkflowSpecHash_stable(t *testing.T) {
	wf := &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindWorkflow,
		Metadata:   spec.Metadata{Name: "demo"},
		Spec: spec.WorkflowSpec{
			Steps: []spec.WorkflowStep{{ID: "a", Uses: "tool.x.y"}},
		},
	}
	h1, err := WorkflowSpecHash(wf)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := WorkflowSpecHash(wf)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == "" || h1 != h2 {
		t.Fatalf("hash %q %q", h1, h2)
	}
}

func TestWorkflowSpecHash_nil(t *testing.T) {
	if _, err := WorkflowSpecHash(nil); err == nil {
		t.Fatal("expected error")
	}
}
