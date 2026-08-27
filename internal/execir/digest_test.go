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
