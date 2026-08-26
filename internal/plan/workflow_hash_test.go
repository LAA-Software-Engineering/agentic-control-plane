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

func TestWorkflowSpecHash_needsIsIdentityAndStable(t *testing.T) {
	wf := func(needs []string) *spec.WorkflowResource {
		return &spec.WorkflowResource{
			APIVersion: spec.APIVersionV0,
			Kind:       spec.KindWorkflow,
			Metadata:   spec.Metadata{Name: "demo"},
			Spec: spec.WorkflowSpec{
				Steps: []spec.WorkflowStep{
					{ID: "a", Uses: "tool.x.y"},
					{ID: "b", Uses: "tool.x.y", Needs: needs},
				},
			},
		}
	}
	h1, err := WorkflowSpecHash(wf([]string{"a"}))
	if err != nil {
		t.Fatal(err)
	}
	h2, err := WorkflowSpecHash(wf([]string{"a"}))
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("needs hash unstable: %s vs %s", h1, h2)
	}
	h3, err := WorkflowSpecHash(wf(nil))
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h3 {
		t.Fatal("needs must participate in workflow spec hash")
	}
	posWF := wf([]string{"a"})
	posWF.Spec.Steps[1].NeedsPos = []spec.Pos{{File: "w.yaml", Line: 4, Column: 2}}
	posWF.Spec.Steps[1].NeedsDeclared = true
	h4, err := WorkflowSpecHash(posWF)
	if err != nil {
		t.Fatal(err)
	}
	if h4 != h1 {
		t.Fatal("NeedsPos must not affect workflow spec hash")
	}
}

func TestWorkflowSpecHash_workflowFieldIsIdentity(t *testing.T) {
	base := &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindWorkflow,
		Metadata:   spec.Metadata{Name: "demo"},
		Spec: spec.WorkflowSpec{
			Steps: []spec.WorkflowStep{{ID: "a", Uses: "tool.x.y"}},
		},
	}
	h1, err := WorkflowSpecHash(base)
	if err != nil {
		t.Fatal(err)
	}
	withCall := &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindWorkflow,
		Metadata:   spec.Metadata{Name: "demo"},
		Spec: spec.WorkflowSpec{
			Steps: []spec.WorkflowStep{{ID: "a", Workflow: "child"}},
		},
	}
	h2, err := WorkflowSpecHash(withCall)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Fatal("workflow: must participate in workflow spec hash")
	}
	withCall.Spec.Steps[0].WorkflowPos = spec.Pos{File: "w.yaml", Line: 4, Column: 2}
	h3, err := WorkflowSpecHash(withCall)
	if err != nil {
		t.Fatal(err)
	}
	if h3 != h2 {
		t.Fatal("WorkflowPos must not affect workflow spec hash")
	}
}
