package execir

import "testing"

func TestRequiresInterpreter(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		prog *Program
		want bool
	}{
		{"nil", nil, false},
		{"straight-line", &Program{Body: []Node{&InvokeTool{Uses: "tool.t.a"}, &Return{Value: Lit{V: 1}}}}, false},
		{"graph", &Program{Body: []Node{&Graph{Nodes: []GraphNode{{ID: "a", Run: &InvokeAgent{Bind: "a", Agent: "A"}}}}}}, false},
		{"fork of straight-line", &Program{Body: []Node{&Fork{Branches: []ForkBranch{{Bind: "x", Nodes: []Node{&InvokeAgent{Bind: "x", Agent: "A"}}}}}}}, false},
		{"branch", &Program{Body: []Node{&Branch{Then: []Node{&InvokeTool{Uses: "tool.t.a"}}}}}, true},
		{"loop", &Program{Body: []Node{&Loop{Var: "i", Body: []Node{&InvokeTool{Uses: "tool.t.a"}}}}}, true},
		{"branch nested in fork", &Program{Body: []Node{&Fork{Branches: []ForkBranch{{Bind: "x", Nodes: []Node{&Branch{}}}}}}}, true},
		{"loop nested in graph node", &Program{Body: []Node{&Graph{Nodes: []GraphNode{{ID: "a", Run: &Loop{Var: "i"}}}}}}, true},
	}
	for _, c := range cases {
		if got := RequiresInterpreter(c.prog); got != c.want {
			t.Errorf("%s: RequiresInterpreter = %v, want %v", c.name, got, c.want)
		}
	}
}
