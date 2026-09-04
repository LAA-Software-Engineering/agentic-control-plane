package execir

import (
	"math"
	"strings"
	"testing"
)

// TestSerialize_UnknownNodeKindFailsClosed proves an unrecognized node kind in a pinned program
// fails decoding rather than being silently dropped — a resumed program missing a step would
// execute something other than what was pinned (S8, issue #392).
func TestSerialize_UnknownNodeKindFailsClosed(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"formatVersion":"agentic.dev/execir/v1","programs":{"w":{"workflow":"w","params":["input"],` +
		`"body":[{"kind":"invokeTool","bind":"a","uses":"tool.x.y"},{"kind":"sleep","seconds":5},{"kind":"return","value":{"kind":"ref","path":["a"]}}]}}}`)
	if _, err := UnmarshalPrograms(payload); err == nil {
		t.Fatal("expected an error for an unknown node kind, got nil (node silently dropped)")
	} else if !strings.Contains(err.Error(), "unknown node kind") {
		t.Fatalf("error should name the unknown node kind, got %v", err)
	}
}

// TestSerialize_UnknownValueKindFailsClosed proves an unknown value kind fails closed rather than
// decoding to a nil literal (#392).
func TestSerialize_UnknownValueKindFailsClosed(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"formatVersion":"agentic.dev/execir/v1","programs":{"w":{"workflow":"w","params":["input"],` +
		`"body":[{"kind":"return","value":{"kind":"futureval","path":["a"]}}]}}}`)
	if _, err := UnmarshalPrograms(payload); err == nil {
		t.Fatal("expected an error for an unknown value kind")
	} else if !strings.Contains(err.Error(), "unknown value kind") {
		t.Fatalf("error should name the unknown value kind, got %v", err)
	}
}

// TestSerialize_UnknownExprKindFailsClosed proves an unknown branch-condition expr kind fails closed
// rather than decoding to an always-false leaf (which silently takes the else arm) (#392).
func TestSerialize_UnknownExprKindFailsClosed(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"formatVersion":"agentic.dev/execir/v1","programs":{"w":{"workflow":"w","params":["input"],` +
		`"body":[{"kind":"branch","cond":{"kind":"futureexpr"},"then":[],"else":[]}]}}}`)
	if _, err := UnmarshalPrograms(payload); err == nil {
		t.Fatal("expected an error for an unknown expr kind")
	} else if !strings.Contains(err.Error(), "unknown expr kind") {
		t.Fatalf("error should name the unknown expr kind, got %v", err)
	}
}

// unknownNode is a Node the serializer does not know how to encode.
type unknownNode struct{}

func (unknownNode) node() {}

// TestSerialize_EncodeUnknownNodeFailsClosed proves MarshalPrograms refuses to pin a program
// containing a node it cannot faithfully encode, rather than writing a lossy "{kind:unknown}"
// artifact whose bytes would not match the in-memory Program.Digest (#392).
func TestSerialize_EncodeUnknownNodeFailsClosed(t *testing.T) {
	t.Parallel()
	progs := map[string]*Program{"w": {Workflow: "w", Body: []Node{unknownNode{}}}}
	if _, err := MarshalPrograms(progs); err == nil {
		t.Fatal("expected MarshalPrograms to refuse an unknown node type")
	} else if !strings.Contains(err.Error(), "cannot encode unknown node type") {
		t.Fatalf("error should name the unencodable node, got %v", err)
	}
}

// TestSerialize_LargeIntLiteralsExact proves integer literals beyond float64's
// exact range (2^53) survive marshal→unmarshal — they must not be laundered
// through float64, or the hydrated program is not the pinned one.
func TestSerialize_LargeIntLiteralsExact(t *testing.T) {
	t.Parallel()
	for _, n := range []int64{5, 1 << 52, (1 << 53) + 1, 1234567890123456789, math.MaxInt64, math.MinInt64} {
		prog := &Program{Workflow: "w", Body: []Node{
			&InvokeTool{Bind: "a", Uses: "tool.t.op", Args: map[string]Value{"n": Lit{V: n}}},
		}}
		got, err := UnmarshalPrograms(mustMarshal(t, map[string]*Program{"w": prog}))
		if err != nil {
			t.Fatalf("n=%d: unmarshal: %v", n, err)
		}
		lit := got["w"].Body[0].(*InvokeTool).Args["n"].(Lit)
		if lit.V != any(n) {
			t.Fatalf("n=%d round-tripped to %#v (type %T)", n, lit.V, lit.V)
		}
		if got["w"].Digest() != prog.Digest() {
			t.Fatalf("n=%d: digest changed on round-trip", n)
		}
	}
}

func mustMarshal(t *testing.T, progs map[string]*Program) []byte {
	t.Helper()
	b, err := MarshalPrograms(progs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// TestSerialize_RoundTripPreservesDigest exercises every node/value/expr kind and
// asserts marshal→unmarshal reconstructs a program with an identical Digest — the
// invariant the snapshot pin relies on (the hydrated program is what was pinned).
func TestSerialize_RoundTripPreservesDigest(t *testing.T) {
	t.Parallel()
	progs := map[string]*Program{
		"flat": {
			Workflow: "flat", Params: []string{"input"},
			Body: []Node{
				&InvokeTool{Bind: "a", Uses: "tool.t.op", Args: map[string]Value{
					"n":    Lit{V: int64(5)},
					"r":    Lit{V: 1.5},
					"f":    Lit{V: true},
					"s":    Lit{V: "hi"},
					"ref":  Ref{Path: []string{"input", "x"}},
					"obj":  Object{Fields: []Field{{Key: "k", Val: Ref{Path: []string{"a"}}}}},
					"list": List{Elems: []Value{Lit{V: "x"}, Ref{Path: []string{"a"}}}},
					"tmpl": Template{Parts: []Value{Lit{V: "p"}, Ref{Path: []string{"a", "y"}}}},
				}},
				&InvokeAgent{Bind: "b", Agent: "reviewer", Args: map[string]Value{"in": Ref{Path: []string{"a"}}}},
				&Let{Bind: "c", Value: Lit{V: int64(7)}},
				&Branch{
					Cond: Not{X: BinOp{Op: ">", X: Leaf{V: Ref{Path: []string{"input", "n"}}}, Y: Leaf{V: Lit{V: int64(3)}}}},
					Then: []Node{&InvokeTool{Bind: "d", Uses: "tool.t.then"}},
					Else: []Node{&Return{Value: Lit{V: "no"}}},
				},
				&Loop{Var: "i", Collection: Ref{Path: []string{"input", "items"}}, Body: []Node{
					&InvokeTool{Uses: "tool.t.iter"},
				}},
				&Fork{Branches: []ForkBranch{{Bind: "x", Nodes: []Node{&InvokeAgent{Bind: "x", Agent: "sec"}}}}},
				&Graph{Nodes: []GraphNode{
					{ID: "g1", Run: &InvokeAgent{Bind: "g1", Agent: "A"}},
					{ID: "g2", Needs: []string{"g1"}, Run: &InvokeWorkflow{Bind: "g2", Workflow: "sub"}},
				}},
				&Approval{Bind: "gate", Description: "review", RedactKeys: []string{"secret"}, Args: map[string]Value{"p": Ref{Path: []string{"a"}}}},
				&Return{Value: Ref{Path: []string{"a"}}},
			},
		},
	}

	payload, err := MarshalPrograms(progs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalPrograms(payload)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != len(progs) {
		t.Fatalf("program count: got %d want %d", len(got), len(progs))
	}
	for name, p := range progs {
		g, ok := got[name]
		if !ok {
			t.Fatalf("missing program %q", name)
		}
		if g.Digest() != p.Digest() {
			t.Fatalf("digest mismatch for %q:\n want %s\n got  %s", name, p.Digest(), g.Digest())
		}
	}
}

// TestSerialize_UnknownFormatRejected proves an unknown format version fails loudly
// (S8): the pinned program must never be reinterpreted under a guessed encoding.
func TestSerialize_UnknownFormatRejected(t *testing.T) {
	t.Parallel()
	if _, err := UnmarshalPrograms([]byte(`{"formatVersion":"agentic.dev/execir/v99","programs":{}}`)); err == nil {
		t.Fatalf("expected an unsupported-format error")
	}
}
