package spec

import (
	"strconv"
	"strings"
	"testing"
)

func TestWorkflowUsesExplicitNeeds_omittedIsSequential(t *testing.T) {
	steps := []WorkflowStep{
		{ID: "a", Uses: "tool.helper.echo"},
		{ID: "b", Uses: "tool.helper.echo"},
	}
	if WorkflowUsesExplicitNeeds(steps) {
		t.Fatal("expected implicit sequential when no needs declared")
	}
	if got := StepNeedsIDs(steps, 0); len(got) != 0 {
		t.Fatalf("root needs = %v", got)
	}
	if got := StepNeedsIDs(steps, 1); len(got) != 1 || got[0] != "a" {
		t.Fatalf("step 1 needs = %v want [a]", got)
	}
}

func TestStepNeedsIDs_graphModeRootsAreIndependent(t *testing.T) {
	steps := []WorkflowStep{
		{ID: "a", Uses: "tool.helper.echo"},
		{ID: "b", Uses: "tool.helper.echo"},
		{ID: "join", Uses: "tool.helper.echo", Needs: []string{"a", "b"}},
	}
	if !WorkflowUsesExplicitNeeds(steps) {
		t.Fatal("expected graph mode")
	}
	if got := StepNeedsIDs(steps, 0); len(got) != 0 {
		t.Fatalf("a needs = %v", got)
	}
	if got := StepNeedsIDs(steps, 1); len(got) != 0 {
		t.Fatalf("b needs = %v", got)
	}
	got := StepNeedsIDs(steps, 2)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("join needs = %v", got)
	}
}

const workflowDanglingNeedsYAML = `apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: demo
spec:
  steps:
    - id: a
      uses: tool.helper.echo
    - id: join
      uses: tool.helper.echo
      needs:
        - missing
`

const workflowCycleNeedsYAML = `apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: demo
spec:
  steps:
    - id: a
      uses: tool.helper.echo
      needs:
        - b
    - id: b
      uses: tool.helper.echo
      needs:
        - a
`

const workflowSelfNeedsYAML = `apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: demo
spec:
  steps:
    - id: a
      uses: tool.helper.echo
      needs:
        - a
`

func TestStampResourcePositions_workflowNeedsLine(t *testing.T) {
	dec, err := ParseResourceFromBytes([]byte(workflowDanglingNeedsYAML), "workflow.yaml")
	if err != nil {
		t.Fatal(err)
	}
	wr := dec.Resource.(*WorkflowResource)
	st := wr.Spec.Steps[1]
	if !st.NeedsDeclared {
		t.Fatal("NeedsDeclared")
	}
	if len(st.NeedsPos) != 1 {
		t.Fatalf("NeedsPos len = %d", len(st.NeedsPos))
	}
	if st.NeedsPos[0].File != "workflow.yaml" || st.NeedsPos[0].Line != 12 {
		t.Fatalf("NeedsPos = %#v, want workflow.yaml line 12", st.NeedsPos[0])
	}
}

func TestValidateWorkflowGraph_table(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		wantSubstr string
		wantLine   int
	}{
		{
			name:       "dangling needs",
			yaml:       workflowDanglingNeedsYAML,
			wantSubstr: `needs references unknown step "missing"`,
			wantLine:   12,
		},
		{
			name:       "cycle",
			yaml:       workflowCycleNeedsYAML,
			wantSubstr: "needs cycle",
			wantLine:   14,
		},
		{
			name:       "self cycle",
			yaml:       workflowSelfNeedsYAML,
			wantSubstr: "needs cycle",
			wantLine:   10,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec, err := ParseResourceFromBytes([]byte(tt.yaml), "workflow.yaml")
			if err != nil {
				t.Fatal(err)
			}
			wr := dec.Resource.(*WorkflowResource)
			g := &ProjectGraph{
				Tools: map[string]*ToolResource{
					"helper": {
						Kind:     KindTool,
						Metadata: Metadata{Name: "helper"},
						Spec:     ToolSpec{Type: "native"},
					},
				},
				Workflows: map[string]*WorkflowResource{"demo": wr},
			}
			err = ValidateProjectGraph(g, t.TempDir())
			if err == nil {
				t.Fatal("expected validation error")
			}
			msg := err.Error()
			if !strings.Contains(msg, tt.wantSubstr) {
				t.Fatalf("want substring %q, got %q", tt.wantSubstr, msg)
			}
			loc := "workflow.yaml:" + strconv.Itoa(tt.wantLine)
			if !strings.Contains(msg, loc) {
				t.Fatalf("want position containing %q, got %q", loc, msg)
			}
		})
	}
}

func TestValidateWorkflowGraph_implicitSequentialOK(t *testing.T) {
	g := &ProjectGraph{
		Tools: map[string]*ToolResource{
			"helper": {
				Kind:     KindTool,
				Metadata: Metadata{Name: "helper"},
				Spec:     ToolSpec{Type: "native"},
			},
		},
		Workflows: map[string]*WorkflowResource{
			"demo": {
				Kind:     KindWorkflow,
				Metadata: Metadata{Name: "demo"},
				Spec: WorkflowSpec{
					Steps: []WorkflowStep{
						{ID: "a", Uses: "tool.helper.echo"},
						{ID: "b", Uses: "tool.helper.echo", With: map[string]any{"x": "${steps.a.output}"}},
					},
				},
			},
		},
	}
	if err := ValidateProjectGraph(g, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateWorkflowStepOrder_siblingInterpolationRejected(t *testing.T) {
	g := &ProjectGraph{
		Tools: map[string]*ToolResource{
			"helper": {
				Kind:     KindTool,
				Metadata: Metadata{Name: "helper"},
				Spec:     ToolSpec{Type: "native"},
			},
		},
		Workflows: map[string]*WorkflowResource{
			"demo": {
				Kind:     KindWorkflow,
				Metadata: Metadata{Name: "demo"},
				Spec: WorkflowSpec{
					Steps: []WorkflowStep{
						{ID: "a", Uses: "tool.helper.echo", With: map[string]any{"x": "${steps.b.output}"}},
						{ID: "b", Uses: "tool.helper.echo"},
						{ID: "join", Uses: "tool.helper.echo", Needs: []string{"a", "b"}},
					},
				},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a predecessor") {
		t.Fatalf("want predecessor error, got %v", err)
	}
}

func TestValidateWorkflowStepOrder_needsAncestorAllowsLaterYAML(t *testing.T) {
	g := &ProjectGraph{
		Tools: map[string]*ToolResource{
			"helper": {
				Kind:     KindTool,
				Metadata: Metadata{Name: "helper"},
				Spec:     ToolSpec{Type: "native"},
			},
		},
		Workflows: map[string]*WorkflowResource{
			"demo": {
				Kind:     KindWorkflow,
				Metadata: Metadata{Name: "demo"},
				Spec: WorkflowSpec{
					Steps: []WorkflowStep{
						{ID: "late", Uses: "tool.helper.echo"},
						{ID: "early", Uses: "tool.helper.echo", Needs: []string{"late"}, With: map[string]any{"x": "${steps.late.output}"}},
					},
				},
			},
		},
	}
	if err := ValidateProjectGraph(g, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
