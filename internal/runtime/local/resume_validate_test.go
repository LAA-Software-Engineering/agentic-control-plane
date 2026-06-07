package local

import (
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

func TestResolveConfigForResume_pinnedAndMatchingCLI(t *testing.T) {
	run := &state.Run{EnvironmentName: "staging"}
	got, err := resolveConfigForResume(run, "staging")
	if err != nil || got != "staging" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestResolveConfigForResume_pinnedIgnoresEmptyCLI(t *testing.T) {
	run := &state.Run{EnvironmentName: "staging"}
	got, err := resolveConfigForResume(run, "")
	if err != nil || got != "staging" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestResolveConfigForResume_conflict(t *testing.T) {
	run := &state.Run{EnvironmentName: "staging"}
	_, err := resolveConfigForResume(run, "prod")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateResumeWorkflowSpec_mismatch(t *testing.T) {
	wf := &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindWorkflow,
		Metadata:   spec.Metadata{Name: "demo"},
		Spec: spec.WorkflowSpec{
			Steps: []spec.WorkflowStep{{ID: "a", Uses: "tool.x.y"}},
		},
	}
	run := &state.Run{WorkflowSpecHash: "deadbeef"}
	if err := validateResumeWorkflowSpec(run, wf); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateResumeWorkflowSpec_legacyEmptyHash(t *testing.T) {
	wf := &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindWorkflow,
		Metadata:   spec.Metadata{Name: "demo"},
	}
	if err := validateResumeWorkflowSpec(&state.Run{}, wf); err != nil {
		t.Fatal(err)
	}
}
