package spec_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/spec"
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

func TestParseResourceFromBytes_approvalTrue(t *testing.T) {
	const y = `
apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: wf
spec:
  steps:
    - id: gate
      approval: true
`
	dec, err := spec.ParseResourceFromBytes([]byte(y), "wf.yaml")
	if err != nil {
		t.Fatal(err)
	}
	wf := dec.Resource.(*spec.WorkflowResource)
	if !spec.StepIsApproval(wf.Spec.Steps[0]) {
		t.Fatalf("got %+v", wf.Spec.Steps[0].Approval)
	}
}

func TestParseResourceFromBytes_approvalValidMapping(t *testing.T) {
	const y = `
apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: wf
spec:
  steps:
    - id: gate
      approval:
        description: hi
        redactKeys: [accountNumber]
`
	dec, err := spec.ParseResourceFromBytes([]byte(y), "wf.yaml")
	if err != nil {
		t.Fatal(err)
	}
	wf := dec.Resource.(*spec.WorkflowResource)
	st := wf.Spec.Steps[0]
	if !spec.StepIsApproval(st) || st.Approval.Config == nil {
		t.Fatalf("got %+v", st.Approval)
	}
	if st.Approval.Config.Description != "hi" {
		t.Fatalf("description %q", st.Approval.Config.Description)
	}
	if len(st.Approval.Config.RedactKeys) != 1 || st.Approval.Config.RedactKeys[0] != "accountNumber" {
		t.Fatalf("redact %+v", st.Approval.Config.RedactKeys)
	}
}

func TestParseResourceFromBytes_approvalUnknownFieldAllowedDecisions(t *testing.T) {
	const y = `
apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: wf
spec:
  steps:
    - id: gate
      approval:
        description: hi
        allowedDecisions: [approve]
`
	_, err := spec.ParseResourceFromBytes([]byte(y), "wf.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, spec.ErrUnknownField) {
		t.Fatalf("want ErrUnknownField in chain, got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, `unknown field "allowedDecisions"`) {
		t.Fatalf("want unknown field message: %q", msg)
	}
}

func TestParseResourceFromBytes_approvalTypoSuggestsRedactKeys(t *testing.T) {
	const y = `
apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: wf
spec:
  steps:
    - id: gate
      approval:
        description: hi
        redactKey: [secret]
`
	_, err := spec.ParseResourceFromBytes([]byte(y), "wf.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, spec.ErrUnknownField) {
		t.Fatalf("want ErrUnknownField in chain, got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, `unknown field "redactKey"`) {
		t.Fatalf("want unknown field message: %q", msg)
	}
	if !strings.Contains(msg, `did you mean "redactKeys"`) {
		t.Fatalf("want suggestion for redactKeys: %q", msg)
	}
}
