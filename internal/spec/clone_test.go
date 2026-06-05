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
