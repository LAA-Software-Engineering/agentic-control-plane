package execir

import "testing"

func boundMap(bs []InvocationBound) map[string]InvocationBound {
	m := map[string]InvocationBound{}
	for _, b := range bs {
		m[b.Kind+":"+b.Callee] = b
	}
	return m
}

// TestInvocationBounds_Flagship: the implement/review loop invokes each agent at
// most `limit` times (#293).
func TestInvocationBounds_Flagship(t *testing.T) {
	prog := &Program{Workflow: "ImplementAndReview", Params: []string{"input"}, Body: []Node{
		&Let{Bind: "state", Value: Ref{Path: []string{"input"}}},
		&While{Cond: Not{X: Leaf{V: Ref{Path: []string{"state", "approved"}}}}, Limit: 3, Body: []Node{
			&InvokeAgent{Bind: "implementation", Agent: "Implementer"},
			&InvokeAgent{Bind: "state", Agent: "Reviewer"},
		}},
		&Return{Value: Ref{Path: []string{"state"}}},
	}}
	m := boundMap(InvocationBounds(prog, 1000))
	if b := m["agent:Implementer"]; b.Max != 3 || b.DataBounded {
		t.Fatalf("Implementer: got %+v, want max 3, not data-bounded", b)
	}
	if b := m["agent:Reviewer"]; b.Max != 3 || b.DataBounded {
		t.Fatalf("Reviewer: got %+v, want max 3", b)
	}
}

// TestInvocationBounds_NestedWhileMultiplies: nested bounded loops multiply.
func TestInvocationBounds_NestedWhileMultiplies(t *testing.T) {
	prog := &Program{Workflow: "W", Body: []Node{
		&While{Cond: Leaf{V: Lit{V: true}}, Limit: 3, Body: []Node{
			&While{Cond: Leaf{V: Lit{V: true}}, Limit: 2, Body: []Node{
				&InvokeAgent{Agent: "A"},
			}},
		}},
	}}
	if b := boundMap(InvocationBounds(prog, 1000))["agent:A"]; b.Max != 6 {
		t.Fatalf("nested 3*2: got %+v, want max 6", b)
	}
}

// TestInvocationBounds_ForIsDataBounded: a for over a runtime collection is not
// statically bounded — it falls back to the global cap and is marked data-bounded.
func TestInvocationBounds_ForIsDataBounded(t *testing.T) {
	prog := &Program{Workflow: "W", Params: []string{"input"}, Body: []Node{
		&Loop{Var: "x", Collection: Ref{Path: []string{"input", "items"}}, Body: []Node{
			&InvokeTool{Uses: "tool.t.op"},
		}},
	}}
	b := boundMap(InvocationBounds(prog, 1000))["tool:tool.t.op"]
	if !b.DataBounded || b.Max != 1000 {
		t.Fatalf("for loop: got %+v, want data-bounded max 1000", b)
	}
}

// TestInvocationBounds_BranchTakesMax: only one arm of a Branch runs, so a callee in
// both arms is bounded by the max, not the sum.
func TestInvocationBounds_BranchTakesMax(t *testing.T) {
	prog := &Program{Workflow: "W", Body: []Node{
		&Branch{
			Cond: Leaf{V: Lit{V: true}},
			Then: []Node{&InvokeAgent{Agent: "A"}},
			Else: []Node{&InvokeAgent{Agent: "A"}, &InvokeAgent{Agent: "B"}},
		},
	}}
	m := boundMap(InvocationBounds(prog, 1000))
	if b := m["agent:A"]; b.Max != 1 {
		t.Fatalf("A across branch arms: got %+v, want max 1 (only one arm runs)", b)
	}
	if b := m["agent:B"]; b.Max != 1 {
		t.Fatalf("B: got %+v, want max 1", b)
	}
}
