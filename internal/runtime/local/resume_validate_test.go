package local

import (
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/plan"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
)

func demoWorkflowForHash() *spec.WorkflowResource {
	return &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindWorkflow,
		Metadata:   spec.Metadata{Name: "demo"},
		Spec: spec.WorkflowSpec{
			Steps: []spec.WorkflowStep{{ID: "a", Uses: "tool.x.y"}},
		},
	}
}

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
	wf := demoWorkflowForHash()
	run := &state.Run{WorkflowSpecHash: "deadbeef"}
	if err := validateResumeWorkflowSpec(run, wf, ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateResumeWorkflowSpec_legacyEmptyHash(t *testing.T) {
	wf := &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindWorkflow,
		Metadata:   spec.Metadata{Name: "demo"},
	}
	if err := validateResumeWorkflowSpec(&state.Run{}, wf, ""); err != nil {
		t.Fatal(err)
	}
}

// TestValidateResumeWorkflowSpec_loweringChangeIsDrift is the #277 acceptance: a
// lowering-only change (same resource projection, different program digest)
// between run-start and resume is reported as drift, while the same program is
// not.
func TestValidateResumeWorkflowSpec_loweringChangeIsDrift(t *testing.T) {
	wf := demoWorkflowForHash()
	storedFolded, err := plan.WorkflowSpecHashWithExec(wf, "program-digest-A")
	if err != nil {
		t.Fatal(err)
	}
	run := &state.Run{WorkflowSpecHash: storedFolded}

	if err := validateResumeWorkflowSpec(run, wf, "program-digest-B"); err == nil {
		t.Fatal("a lowering-only change (same resource, different program digest) must be reported as drift")
	}
	if err := validateResumeWorkflowSpec(run, wf, "program-digest-A"); err != nil {
		t.Fatalf("the same pinned program must not be drift: %v", err)
	}
}

// TestValidateResumeWorkflowSpec_legacyBareHashResumes proves the migration: a run
// row created before #277 stored the resource-only hash and must still resume even
// though resume now folds a program digest.
func TestValidateResumeWorkflowSpec_legacyBareHashResumes(t *testing.T) {
	wf := demoWorkflowForHash()
	bare, err := plan.WorkflowSpecHash(wf)
	if err != nil {
		t.Fatal(err)
	}
	run := &state.Run{WorkflowSpecHash: bare}
	if err := validateResumeWorkflowSpec(run, wf, "program-digest-A"); err != nil {
		t.Fatalf("a legacy bare-hash run must still resume: %v", err)
	}
}
