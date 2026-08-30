package execir

import "testing"

func TestDigest_StableAndSensitive(t *testing.T) {
	t.Parallel()
	base := func() *Program {
		return &Program{
			Workflow: "W",
			Params:   []string{"input"},
			Body: []Node{
				&Branch{
					Cond: BinOp{Op: ">", X: Leaf{V: Ref{Path: []string{"input", "n"}}}, Y: Leaf{V: Lit{V: int64(5)}}},
					Then: []Node{&InvokeTool{Bind: "a", Uses: "tool.t.big"}},
					Else: []Node{&InvokeTool{Bind: "a", Uses: "tool.t.small"}},
				},
			},
		}
	}
	// Positions must not affect the digest.
	p1 := base()
	p2 := base()
	p2.Body[0].(*Branch).Pos = Pos{Line: 99, Column: 3}
	if p1.Digest() != p2.Digest() {
		t.Fatalf("digest should ignore positions")
	}

	// A change in control-flow structure (swapped arms) must change the digest.
	p3 := base()
	br := p3.Body[0].(*Branch)
	br.Then, br.Else = br.Else, br.Then
	if p1.Digest() == p3.Digest() {
		t.Fatalf("digest should change when the two arms differ in structure")
	}

	// A change in an invoked operation must change the digest.
	p4 := base()
	p4.Body[0].(*Branch).Then[0].(*InvokeTool).Uses = "tool.t.other"
	if p1.Digest() == p4.Digest() {
		t.Fatalf("digest should change when an invoked operation changes")
	}

	// A literal's type must matter (1 the int vs "1" the string).
	p5 := base()
	p5.Body[0].(*Branch).Cond = BinOp{Op: ">", X: Leaf{V: Ref{Path: []string{"input", "n"}}}, Y: Leaf{V: Lit{V: "5"}}}
	if p1.Digest() == p5.Digest() {
		t.Fatalf("digest should distinguish literal types")
	}
}

// TestDigest_GraphCanonicalAndSensitive covers the #256 Graph node: authored
// node order and per-node needs order are canonicalized away (a semantically-
// equivalent structuring shares a digest), while a changed edge or operation
// still moves it.
func TestDigest_GraphCanonicalAndSensitive(t *testing.T) {
	t.Parallel()
	graph := func(nodes []GraphNode) *Program {
		return &Program{Workflow: "W", Params: []string{"input"}, Body: []Node{&Graph{Nodes: nodes}}}
	}
	a := graph([]GraphNode{
		{ID: "a", Run: &InvokeAgent{Bind: "a", Agent: "A"}},
		{ID: "b", Run: &InvokeAgent{Bind: "b", Agent: "B"}},
		{ID: "d", Needs: []string{"a", "b"}, Run: &InvokeAgent{Bind: "d", Agent: "D"}},
	})
	// Reordered nodes and reordered needs — same DAG.
	b := graph([]GraphNode{
		{ID: "d", Needs: []string{"b", "a"}, Run: &InvokeAgent{Bind: "d", Agent: "D"}},
		{ID: "b", Run: &InvokeAgent{Bind: "b", Agent: "B"}},
		{ID: "a", Run: &InvokeAgent{Bind: "a", Agent: "A"}},
	})
	if a.Digest() != b.Digest() {
		t.Fatalf("digest should ignore node/needs ordering")
	}
	// A changed edge must move the digest.
	c := graph([]GraphNode{
		{ID: "a", Run: &InvokeAgent{Bind: "a", Agent: "A"}},
		{ID: "b", Run: &InvokeAgent{Bind: "b", Agent: "B"}},
		{ID: "d", Needs: []string{"a"}, Run: &InvokeAgent{Bind: "d", Agent: "D"}},
	})
	if a.Digest() == c.Digest() {
		t.Fatalf("digest should change when a needs edge is dropped")
	}
	// A flat sequential program is NOT the same as a Graph of the same invokes.
	flat := &Program{Workflow: "W", Params: []string{"input"}, Body: []Node{
		&InvokeAgent{Bind: "a", Agent: "A"}, &InvokeAgent{Bind: "b", Agent: "B"},
	}}
	twoNodeGraph := graph([]GraphNode{
		{ID: "a", Run: &InvokeAgent{Bind: "a", Agent: "A"}},
		{ID: "b", Run: &InvokeAgent{Bind: "b", Agent: "B"}},
	})
	if flat.Digest() == twoNodeGraph.Digest() {
		t.Fatalf("a flat node list and a Graph must be distinguishable")
	}
}

// TestDigest_CompositeValuesAndApproval covers the new composite Value forms and
// the Approval node: field order is canonicalized, list order and approval
// presentation are significant, and distinct value kinds do not collide.
func TestDigest_CompositeValuesAndApproval(t *testing.T) {
	t.Parallel()
	ret := func(v Value) *Program {
		return &Program{Workflow: "W", Body: []Node{&Return{Value: v}}}
	}
	obj1 := ret(Object{Fields: []Field{{Key: "x", Val: Lit{V: int64(1)}}, {Key: "y", Val: Ref{Path: []string{"a"}}}}})
	obj2 := ret(Object{Fields: []Field{{Key: "y", Val: Ref{Path: []string{"a"}}}, {Key: "x", Val: Lit{V: int64(1)}}}})
	if obj1.Digest() != obj2.Digest() {
		t.Fatalf("object field order must not affect the digest")
	}
	list1 := ret(List{Elems: []Value{Lit{V: int64(1)}, Lit{V: int64(2)}}})
	list2 := ret(List{Elems: []Value{Lit{V: int64(2)}, Lit{V: int64(1)}}})
	if list1.Digest() == list2.Digest() {
		t.Fatalf("list element order must affect the digest")
	}
	// An Object with one "value" field must not collide with a bare value: the
	// YAML unwrap distinction has to survive into the digest.
	if obj := ret(Object{Fields: []Field{{Key: "value", Val: Ref{Path: []string{"a"}}}}}); obj.Digest() == ret(Ref{Path: []string{"a"}}).Digest() {
		t.Fatalf("Object{value:X} must not digest-collide with a bare X")
	}
	appr1 := &Program{Workflow: "W", Body: []Node{&Approval{Bind: "g", Description: "review", RedactKeys: []string{"a", "b"}}}}
	appr2 := &Program{Workflow: "W", Body: []Node{&Approval{Bind: "g", Description: "review", RedactKeys: []string{"b", "a"}}}}
	if appr1.Digest() != appr2.Digest() {
		t.Fatalf("approval redactKeys order must not affect the digest")
	}
	appr3 := &Program{Workflow: "W", Body: []Node{&Approval{Bind: "g", Description: "changed", RedactKeys: []string{"a", "b"}}}}
	if appr1.Digest() == appr3.Digest() {
		t.Fatalf("approval description must affect the digest")
	}
}
