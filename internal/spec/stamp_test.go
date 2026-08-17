package spec

import (
	"errors"
	"strings"
	"testing"
)

const workflowUnknownAgentYAML = `apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: demo
spec:
  steps:
    - id: ping
      agent: missing-bot
`

func TestStampResourcePositions_workflowAgentLine(t *testing.T) {
	dec, err := ParseResourceFromBytes([]byte(workflowUnknownAgentYAML), "workflow.yaml")
	if err != nil {
		t.Fatal(err)
	}
	wr, ok := dec.Resource.(*WorkflowResource)
	if !ok || wr == nil {
		t.Fatalf("got %T", dec.Resource)
	}
	if wr.Pos.File != "workflow.yaml" || wr.Pos.Line != 1 || wr.Pos.Column != 1 {
		t.Fatalf("resource Pos = %#v", wr.Pos)
	}
	if len(wr.Spec.Steps) != 1 {
		t.Fatalf("steps = %d", len(wr.Spec.Steps))
	}
	st := wr.Spec.Steps[0]
	if st.Pos.Line != 7 {
		t.Fatalf("step Pos = %#v, want line 7", st.Pos)
	}
	if st.AgentPos.File != "workflow.yaml" || st.AgentPos.Line != 8 {
		t.Fatalf("AgentPos = %#v, want workflow.yaml line 8", st.AgentPos)
	}
	if st.AgentPos.Column <= 0 {
		t.Fatalf("AgentPos.Column = %d, want yaml.Node column", st.AgentPos.Column)
	}
}

func TestValidateProjectGraph_unknownAgentReportsPos(t *testing.T) {
	dec, err := ParseResourceFromBytes([]byte(workflowUnknownAgentYAML), "workflow.yaml")
	if err != nil {
		t.Fatal(err)
	}
	wr := dec.Resource.(*WorkflowResource)
	g := &ProjectGraph{
		Workflows: map[string]*WorkflowResource{"demo": wr},
	}
	err = ValidateProjectGraph(g, t.TempDir())
	if err == nil {
		t.Fatal("expected missing agent error")
	}
	msg := err.Error()
	want := wr.Spec.Steps[0].AgentPos.String()
	if want == "" || !strings.Contains(msg, want+": Workflow/demo references missing Agent/missing-bot") {
		t.Fatalf("want positioned MissingRefError containing %q, got %q", want, msg)
	}
	var mr *MissingRefError
	if !errors.As(err, &mr) {
		t.Fatalf("want *MissingRefError in chain, got %v", err)
	}
	if mr.Pos != wr.Spec.Steps[0].AgentPos {
		t.Fatalf("MissingRefError.Pos = %#v, want %#v", mr.Pos, wr.Spec.Steps[0].AgentPos)
	}
}

const policyItemPosYAML = `apiVersion: agentic.dev/v0
kind: Policy
metadata:
  name: default
spec:
  approvals:
    requiredFor:
      - tool.missing.op
  hitl:
    interruptOn:
      deploy: true
  effects:
    permit:
      - github.read
    permitWithApproval:
      - destructive
`

func TestStampResourcePositions_policyRequiredForAndInterruptOn(t *testing.T) {
	dec, err := ParseResourceFromBytes([]byte(policyItemPosYAML), "policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	pr, ok := dec.Resource.(*PolicyResource)
	if !ok || pr == nil {
		t.Fatalf("got %T", dec.Resource)
	}
	if pr.Spec.Approvals == nil || len(pr.Spec.Approvals.RequiredForPos) != 1 {
		t.Fatalf("RequiredForPos = %#v", pr.Spec.Approvals)
	}
	rp := pr.Spec.Approvals.RequiredForPos[0]
	if rp.File != "policy.yaml" || rp.Line != 8 {
		t.Fatalf("requiredFor item Pos = %#v, want policy.yaml line 8", rp)
	}
	if pr.Spec.Hitl == nil {
		t.Fatal("expected hitl")
	}
	ip := pr.Spec.Hitl.InterruptOnPos["deploy"]
	if ip.File != "policy.yaml" || ip.Line != 11 {
		t.Fatalf("interruptOn key Pos = %#v, want policy.yaml line 11", ip)
	}
	if pr.Spec.Effects == nil || len(pr.Spec.Effects.PermitPos) != 1 || pr.Spec.Effects.PermitPos[0].Line < 11 {
		t.Fatalf("PermitPos = %#v", pr.Spec.Effects)
	}
	if len(pr.Spec.Effects.PermitWithApprovalPos) != 1 || pr.Spec.Effects.PermitWithApprovalPos[0].Line < 11 {
		t.Fatalf("PermitWithApprovalPos = %#v", pr.Spec.Effects)
	}
}
