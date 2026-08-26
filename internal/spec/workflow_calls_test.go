package spec

import (
	"strconv"
	"strings"
	"testing"
)

func TestValidateProjectGraph_danglingWorkflowRefReportsPosition(t *testing.T) {
	const yaml = `apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: parent
spec:
  steps:
    - id: call
      workflow: missing
`
	dec, err := ParseResourceFromBytes([]byte(yaml), "parent.yaml")
	if err != nil {
		t.Fatal(err)
	}
	wr := dec.Resource.(*WorkflowResource)
	g := &ProjectGraph{
		Workflows: map[string]*WorkflowResource{"parent": wr},
	}
	err = ValidateProjectGraph(g, t.TempDir())
	if err == nil {
		t.Fatal("expected missing workflow")
	}
	msg := err.Error()
	pos := wr.Spec.Steps[0].WorkflowPos.String()
	if pos == "" || !strings.Contains(msg, pos) {
		t.Fatalf("want positioned error containing %q, got %q", pos, msg)
	}
	if !strings.Contains(msg, "Workflow/missing") {
		t.Fatalf("got %q", msg)
	}
}

func TestValidateProjectGraph_directWorkflowRecursionReportsPosition(t *testing.T) {
	const yaml = `apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: loop
spec:
  steps:
    - id: again
      workflow: loop
`
	dec, err := ParseResourceFromBytes([]byte(yaml), "loop.yaml")
	if err != nil {
		t.Fatal(err)
	}
	wr := dec.Resource.(*WorkflowResource)
	g := &ProjectGraph{Workflows: map[string]*WorkflowResource{"loop": wr}}
	err = ValidateProjectGraph(g, t.TempDir())
	if err == nil {
		t.Fatal("expected cycle")
	}
	msg := err.Error()
	pos := wr.Spec.Steps[0].WorkflowPos.String()
	if pos == "" || !strings.Contains(msg, pos) {
		t.Fatalf("want position %q in %q", pos, msg)
	}
	if !strings.Contains(msg, "workflow call cycle") {
		t.Fatalf("got %q", msg)
	}
}

func TestValidateProjectGraph_mutualWorkflowRecursionReportsPosition(t *testing.T) {
	leftYAML := `apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: left
spec:
  steps:
    - id: to_right
      workflow: right
`
	rightYAML := `apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: right
spec:
  steps:
    - id: to_left
      workflow: left
`
	left, err := ParseResourceFromBytes([]byte(leftYAML), "left.yaml")
	if err != nil {
		t.Fatal(err)
	}
	right, err := ParseResourceFromBytes([]byte(rightYAML), "right.yaml")
	if err != nil {
		t.Fatal(err)
	}
	g := &ProjectGraph{
		Workflows: map[string]*WorkflowResource{
			"left":  left.Resource.(*WorkflowResource),
			"right": right.Resource.(*WorkflowResource),
		},
	}
	err = ValidateProjectGraph(g, t.TempDir())
	if err == nil {
		t.Fatal("expected cycle")
	}
	msg := err.Error()
	if !strings.Contains(msg, "workflow call cycle") {
		t.Fatalf("got %q", msg)
	}
	lp := left.Resource.(*WorkflowResource).Spec.Steps[0].WorkflowPos.String()
	rp := right.Resource.(*WorkflowResource).Spec.Steps[0].WorkflowPos.String()
	if !strings.Contains(msg, lp) && !strings.Contains(msg, rp) {
		t.Fatalf("want a call-site position (%q or %q) in %q", lp, rp, msg)
	}
}

func TestValidateProjectGraph_workflowNestingDepth(t *testing.T) {
	root := t.TempDir()
	g := chainWorkflows(t, DefaultMaxWorkflowNesting+1)
	g.Spec.Limits = &ExecutionLimits{MaxWorkflowNesting: 2}
	err := ValidateProjectGraph(g, root)
	if err == nil {
		t.Fatal("expected depth error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "maxWorkflowNesting") {
		t.Fatalf("got %q", msg)
	}
	if !strings.Contains(msg, "exceeds") {
		t.Fatalf("got %q", msg)
	}
}

func TestValidateProjectGraph_workflowNestingDepthLongerPath(t *testing.T) {
	// A short path to mid must not hide a longer path that exceeds the cap.
	helper := &ToolResource{Kind: KindTool, Metadata: Metadata{Name: "helper"}, Spec: ToolSpec{Type: "native"}}
	wf := func(name string, st WorkflowStep) *WorkflowResource {
		return &WorkflowResource{Kind: KindWorkflow, Metadata: Metadata{Name: name}, Spec: WorkflowSpec{Steps: []WorkflowStep{st}}}
	}
	pos := func(line int) Pos { return Pos{File: "w.yaml", Line: line, Column: 5} }
	g := &ProjectGraph{
		Spec:  ProjectSpec{Limits: &ExecutionLimits{MaxWorkflowNesting: 3}},
		Tools: map[string]*ToolResource{"helper": helper},
		Workflows: map[string]*WorkflowResource{
			"w0": wf("w0", WorkflowStep{ID: "a", Workflow: "wa", WorkflowPos: pos(1)}),
			"wa": wf("wa", WorkflowStep{ID: "d", Workflow: "wd", WorkflowPos: pos(2)}),
			"wb": wf("wb", WorkflowStep{ID: "c", Workflow: "wc", WorkflowPos: pos(3)}),
			"wc": wf("wc", WorkflowStep{ID: "d", Workflow: "wd", WorkflowPos: pos(4)}),
			"wd": wf("wd", WorkflowStep{ID: "e", Workflow: "we", WorkflowPos: pos(5)}),
			"we": wf("we", WorkflowStep{ID: "n", Uses: "tool.helper.echo"}),
		},
	}
	// Second root edge from w0 so both paths exist.
	g.Workflows["w0"].Spec.Steps = []WorkflowStep{
		{ID: "a", Workflow: "wa", WorkflowPos: pos(1)},
		{ID: "b", Workflow: "wb", WorkflowPos: pos(6)},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil {
		t.Fatal("expected depth error on longer path")
	}
	msg := err.Error()
	if !strings.Contains(msg, "maxWorkflowNesting") || !strings.Contains(msg, "exceeds") {
		t.Fatalf("got %q", msg)
	}
	if !strings.Contains(msg, pos(5).String()) {
		t.Fatalf("want position of wd->we call in %q", msg)
	}
}

func TestValidateProjectGraph_workflowCallOK(t *testing.T) {
	childYAML := `apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: child
spec:
  steps:
    - id: echo
      uses: tool.helper.echo
`
	parentYAML := `apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: parent
spec:
  steps:
    - id: call
      workflow: child
      with:
        x: ${input.x}
`
	child, err := ParseResourceFromBytes([]byte(childYAML), "child.yaml")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := ParseResourceFromBytes([]byte(parentYAML), "parent.yaml")
	if err != nil {
		t.Fatal(err)
	}
	g := &ProjectGraph{
		Tools: map[string]*ToolResource{
			"helper": {Kind: KindTool, Metadata: Metadata{Name: "helper"}, Spec: ToolSpec{Type: "native"}},
		},
		Workflows: map[string]*WorkflowResource{
			"child":  child.Resource.(*WorkflowResource),
			"parent": parent.Resource.(*WorkflowResource),
		},
	}
	if err := ValidateProjectGraph(g, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateProjectGraph_workflowPlusAgentRejected(t *testing.T) {
	g := &ProjectGraph{
		Agents: map[string]*AgentResource{
			"a": {Kind: KindAgent, Metadata: Metadata{Name: "a"}},
		},
		Workflows: map[string]*WorkflowResource{
			"w": {
				Kind:     KindWorkflow,
				Metadata: Metadata{Name: "w"},
				Spec: WorkflowSpec{
					Steps: []WorkflowStep{{ID: "s", Agent: "a", Workflow: "w"}},
				},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "more than one of agent, uses, or workflow") {
		t.Fatalf("got %v", err)
	}
}

func chainWorkflows(t *testing.T, n int) *ProjectGraph {
	t.Helper()
	wfs := make(map[string]*WorkflowResource, n)
	for i := 0; i < n; i++ {
		name := "w" + strconv.Itoa(i)
		st := WorkflowStep{ID: "s", Uses: "tool.helper.echo"}
		if i+1 < n {
			st = WorkflowStep{ID: "s", Workflow: "w" + strconv.Itoa(i+1), WorkflowPos: Pos{File: "w.yaml", Line: i + 1, Column: 5}}
		}
		wfs[name] = &WorkflowResource{
			Kind:     KindWorkflow,
			Metadata: Metadata{Name: name},
			Spec:     WorkflowSpec{Steps: []WorkflowStep{st}},
		}
	}
	return &ProjectGraph{
		Tools: map[string]*ToolResource{
			"helper": {Kind: KindTool, Metadata: Metadata{Name: "helper"}, Spec: ToolSpec{Type: "native"}},
		},
		Workflows: wfs,
	}
}
