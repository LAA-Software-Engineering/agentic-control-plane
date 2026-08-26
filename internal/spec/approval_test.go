package spec_test

import (
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"gopkg.in/yaml.v3"
)

func TestWorkflowApprovalValue_unmarshalTrue(t *testing.T) {
	var st spec.WorkflowStep
	if err := yaml.Unmarshal([]byte("id: gate\napproval: true\n"), &st); err != nil {
		t.Fatal(err)
	}
	if !spec.StepIsApproval(st) {
		t.Fatalf("got %+v", st.Approval)
	}
	if st.Approval.Config != nil {
		t.Fatalf("true should have nil config: %+v", st.Approval)
	}
}

func TestWorkflowApprovalValue_unmarshalConfig(t *testing.T) {
	raw := `
id: gate
approval:
  description: Approve the plan
  redactKeys: [secret]
`
	var st spec.WorkflowStep
	if err := yaml.Unmarshal([]byte(raw), &st); err != nil {
		t.Fatal(err)
	}
	if !spec.StepIsApproval(st) || st.Approval.Config == nil {
		t.Fatalf("got %+v", st.Approval)
	}
	if st.Approval.Config.Description != "Approve the plan" {
		t.Fatalf("description %q", st.Approval.Config.Description)
	}
	if len(st.Approval.Config.RedactKeys) != 1 || st.Approval.Config.RedactKeys[0] != "secret" {
		t.Fatalf("redact %+v", st.Approval.Config.RedactKeys)
	}
}

func TestWorkflowApprovalValue_unmarshalFalseRejected(t *testing.T) {
	var st spec.WorkflowStep
	err := yaml.Unmarshal([]byte("id: gate\napproval: false\n"), &st)
	if err == nil || !strings.Contains(err.Error(), "false") {
		t.Fatalf("expected false rejection, got %v", err)
	}
}

func TestApprovalStepDescription_default(t *testing.T) {
	st := spec.WorkflowStep{Approval: &spec.WorkflowApprovalValue{Enabled: true}}
	if got := spec.ApprovalStepDescription(st); got != spec.DefaultApprovalStepDescription {
		t.Fatalf("got %q", got)
	}
}
