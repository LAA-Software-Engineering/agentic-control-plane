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
