package spec

import "testing"

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
						ID:       "ping",
						Agent:    "a",
						Pos:      Pos{File: "workflow.yaml", Line: 8, Column: 5},
						AgentPos: Pos{File: "workflow.yaml", Line: 9, Column: 14},
					}},
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
	cl.Agents["a"].Pos.Line = 99
	if g.Agents["a"].Pos.Line != 1 {
		t.Fatal("clone Pos aliased original")
	}
}
