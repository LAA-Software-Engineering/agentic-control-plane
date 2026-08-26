package spec

import (
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/schema"
)

func TestCloneProjectGraph_isolatesMutation(t *testing.T) {
	g := &ProjectGraph{
		Meta: Metadata{Name: "demo"},
		Spec: ProjectSpec{
			Defaults: &ProjectDefaults{Model: "openai/gpt-4"},
		},
		Agents: map[string]*AgentResource{
			"a": {
				APIVersion: APIVersionV0,
				Kind:       KindAgent,
				Metadata:   Metadata{Name: "a"},
				Spec:       AgentSpec{Model: "before"},
			},
		},
	}
	cl, err := CloneProjectGraph(g)
	if err != nil {
		t.Fatal(err)
	}
	cl.Agents["a"].Spec.Model = "after"
	if g.Agents["a"].Spec.Model != "before" {
		t.Fatalf("original mutated: %q", g.Agents["a"].Spec.Model)
	}
	if cl.Meta.Name != g.Meta.Name {
		t.Fatalf("Meta.Name = %q, want %q", cl.Meta.Name, g.Meta.Name)
	}
}

func TestCloneProjectGraph_preservesPos(t *testing.T) {
	g := &ProjectGraph{
		Meta: Metadata{Name: "demo"},
		Pos:  Pos{File: "project.yaml", Line: 1, Column: 1},
		Agents: map[string]*AgentResource{
			"a": {
				APIVersion: APIVersionV0,
				Kind:       KindAgent,
				Metadata:   Metadata{Name: "a"},
				Spec: AgentSpec{
					Tools:    []string{"helper"},
					ToolsPos: []Pos{{File: "agent.yaml", Line: 10, Column: 5}},
				},
				Pos: Pos{File: "agent.yaml", Line: 1, Column: 1},
			},
		},
		Workflows: map[string]*WorkflowResource{
			"w": {
				APIVersion: APIVersionV0,
				Kind:       KindWorkflow,
				Metadata:   Metadata{Name: "w"},
				Pos:        Pos{File: "workflow.yaml", Line: 1, Column: 1},
				Spec: WorkflowSpec{
					Steps: []WorkflowStep{{
						ID:            "ping",
						Agent:         "a",
						Needs:         []string{"setup"},
						Pos:           Pos{File: "workflow.yaml", Line: 8, Column: 5},
						AgentPos:      Pos{File: "workflow.yaml", Line: 9, Column: 14},
						WorkflowPos:   Pos{File: "workflow.yaml", Line: 11, Column: 16},
						NeedsPos:      []Pos{{File: "workflow.yaml", Line: 10, Column: 9}},
						NeedsDeclared: true,
					}},
				},
			},
		},
		Policies: map[string]*PolicyResource{
			"default": {
				APIVersion: APIVersionV0,
				Kind:       KindPolicy,
				Metadata:   Metadata{Name: "default"},
				Pos:        Pos{File: "policy.yaml", Line: 1, Column: 1},
				Spec: PolicySpec{
					Approvals: &PolicyApprovals{
						RequiredFor:    []string{"tool.helper.echo"},
						RequiredForPos: []Pos{{File: "policy.yaml", Line: 8, Column: 7}},
					},
					Hitl: &HitlPolicy{
						InterruptOn: map[string]HitlInterruptValue{
							"helper": {Enabled: true},
						},
						InterruptOnPos: map[string]Pos{
							"helper": {File: "policy.yaml", Line: 12, Column: 5},
						},
					},
					Effects: &PolicyEffects{
						Permit:    []string{"github.read"},
						PermitPos: []Pos{{File: "policy.yaml", Line: 16, Column: 7}},
					},
				},
			},
		},
		Tools: map[string]*ToolResource{
			"helper": {
				APIVersion: APIVersionV0,
				Kind:       KindTool,
				Metadata:   Metadata{Name: "helper"},
				Pos:        Pos{File: "helper.yaml", Line: 1, Column: 1},
				Spec: ToolSpec{
					Type: "native",
					Operations: map[string]ToolOperation{
						"echo": {
							Effects:    []string{"github.read"},
							Pos:        Pos{File: "helper.yaml", Line: 8, Column: 3},
							EffectsPos: []Pos{{File: "helper.yaml", Line: 9, Column: 16}},
						},
					},
				},
			},
		},
	}
	cl, err := CloneProjectGraph(g)
	if err != nil {
		t.Fatal(err)
	}
	if cl.Pos != g.Pos {
		t.Fatalf("graph Pos = %#v, want %#v", cl.Pos, g.Pos)
	}
	if cl.Agents["a"].Pos != g.Agents["a"].Pos {
		t.Fatalf("agent Pos dropped: %#v", cl.Agents["a"].Pos)
	}
	if len(cl.Agents["a"].Spec.ToolsPos) != 1 || cl.Agents["a"].Spec.ToolsPos[0].Line != 10 {
		t.Fatalf("ToolsPos = %#v", cl.Agents["a"].Spec.ToolsPos)
	}
	st := cl.Workflows["w"].Spec.Steps[0]
	if st.AgentPos.Line != 9 || st.Pos.Line != 8 {
		t.Fatalf("step pos dropped: %#v", st)
	}
	if st.WorkflowPos.Line != 11 {
		t.Fatalf("WorkflowPos dropped: %#v", st)
	}
	if !st.NeedsDeclared || len(st.NeedsPos) != 1 || st.NeedsPos[0].Line != 10 {
		t.Fatalf("NeedsPos dropped: %#v", st)
	}
	pol := cl.Policies["default"]
	if pol.Spec.Approvals == nil || len(pol.Spec.Approvals.RequiredForPos) != 1 || pol.Spec.Approvals.RequiredForPos[0].Line != 8 {
		t.Fatalf("RequiredForPos dropped: %#v", pol.Spec.Approvals)
	}
	if pol.Spec.Hitl == nil || pol.Spec.Hitl.InterruptOnPos["helper"].Line != 12 {
		t.Fatalf("InterruptOnPos dropped: %#v", pol.Spec.Hitl)
	}
	if pol.Spec.Effects == nil || len(pol.Spec.Effects.PermitPos) != 1 || pol.Spec.Effects.PermitPos[0].Line != 16 {
		t.Fatalf("PermitPos dropped: %#v", pol.Spec.Effects)
	}
	op := cl.Tools["helper"].Spec.Operations["echo"]
	if op.Pos.Line != 8 || len(op.EffectsPos) != 1 || op.EffectsPos[0].Line != 9 {
		t.Fatalf("tool operation Pos dropped: %#v", op)
	}
	cl.Agents["a"].Pos.Line = 99
	if g.Agents["a"].Pos.Line != 1 {
		t.Fatal("clone Pos aliased original")
	}
}

func TestCloneProjectGraph_preservesResolvedSchema(t *testing.T) {
	doc := &schema.Document{Path: "/tmp/in.json", Raw: map[string]any{"type": "object"}}
	g := &ProjectGraph{
		Agents: map[string]*AgentResource{
			"a": {
				Kind:     KindAgent,
				Metadata: Metadata{Name: "a"},
				Spec: AgentSpec{
					Input:  &AgentIO{Schema: "./schemas/in.json", Resolved: doc},
					Output: &AgentIO{Schema: "./schemas/out.json", Resolved: doc},
				},
			},
		},
		Workflows: map[string]*WorkflowResource{
			"w": {
				Kind:     KindWorkflow,
				Metadata: Metadata{Name: "w"},
				Spec: WorkflowSpec{
					Input: &WorkflowInput{Schema: "./schemas/in.json", Resolved: doc},
				},
			},
		},
	}
	cl, err := CloneProjectGraph(g)
	if err != nil {
		t.Fatal(err)
	}
	if cl.Agents["a"].Spec.Input.Resolved != doc || cl.Agents["a"].Spec.Output.Resolved != doc {
		t.Fatal("agent Resolved schema dropped on clone")
	}
	if cl.Workflows["w"].Spec.Input.Resolved != doc {
		t.Fatal("workflow input Resolved schema dropped on clone")
	}
}
