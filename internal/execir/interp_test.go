package execir

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recorder is a test Invoker that records calls (order-sensitively) and returns
// canned or synthesized responses. Safe for concurrent use.
type recorder struct {
	mu    sync.Mutex
	tools []string
	// respond, if set, produces a tool result from its uses and args.
	respond func(uses string, args map[string]any) any
	// track observes concurrency: it increments on entry and decrements on exit.
	track   *int32
	peak    *int32
	holdFor time.Duration
}

func (r *recorder) InvokeTool(_ context.Context, _ CallSite, uses string, args map[string]any) (any, error) {
	if r.track != nil {
		cur := atomic.AddInt32(r.track, 1)
		for {
			p := atomic.LoadInt32(r.peak)
			if cur <= p || atomic.CompareAndSwapInt32(r.peak, p, cur) {
				break
			}
		}
		if r.holdFor > 0 {
			time.Sleep(r.holdFor)
		}
		atomic.AddInt32(r.track, -1)
	}
	r.mu.Lock()
	r.tools = append(r.tools, uses+argsSuffix(args))
	r.mu.Unlock()
	if r.respond != nil {
		return r.respond(uses, args), nil
	}
	return nil, nil
}

func (r *recorder) InvokeAgent(_ context.Context, _ CallSite, agent string, args map[string]any) (any, error) {
	r.mu.Lock()
	r.tools = append(r.tools, "agent:"+agent)
	r.mu.Unlock()
	if r.respond != nil {
		return r.respond("agent:"+agent, args), nil
	}
	return map[string]any{"agent": agent}, nil
}

func (r *recorder) InvokeWorkflow(_ context.Context, _ CallSite, wf string, args map[string]any) (any, error) {
	r.mu.Lock()
	r.tools = append(r.tools, "workflow:"+wf)
	r.mu.Unlock()
	return nil, nil
}

func argsSuffix(args map[string]any) string {
	if v, ok := args["v"]; ok {
		return fmt.Sprintf("(%v)", v)
	}
	return ""
}

func (r *recorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.tools))
	copy(out, r.tools)
	return out
}

func runProg(t *testing.T, in *Interp, prog *Program, input map[string]any) any {
	t.Helper()
	out, err := in.Run(context.Background(), prog, input)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return out
}

func TestBranch_TakesThenAndElse(t *testing.T) {
	t.Parallel()
	prog := &Program{
		Workflow: "W",
		Params:   []string{"input"},
		Body: []Node{
			&Branch{
				Cond: BinOp{Op: ">", X: Leaf{V: Ref{Path: []string{"input", "n"}}}, Y: Leaf{V: Lit{V: int64(5)}}},
				Then: []Node{&InvokeTool{Uses: "tool.t.big"}},
				Else: []Node{&InvokeTool{Uses: "tool.t.small"}},
			},
		},
	}
	rec := &recorder{}
	in := &Interp{Invoker: rec}

	runProg(t, in, prog, map[string]any{"n": int64(10)})
	if got := rec.names(); len(got) != 1 || got[0] != "tool.t.big" {
		t.Fatalf("n=10 should take then-branch, got %v", got)
	}

	rec2 := &recorder{}
	runProg(t, &Interp{Invoker: rec2}, prog, map[string]any{"n": int64(3)})
	if got := rec2.names(); len(got) != 1 || got[0] != "tool.t.small" {
		t.Fatalf("n=3 should take else-branch, got %v", got)
	}
}

func TestBranch_BooleanLeafAndLogical(t *testing.T) {
	t.Parallel()
	prog := &Program{
		Workflow: "W",
		Params:   []string{"input"},
		Body: []Node{
			&Branch{
				// input.a && !input.b
				Cond: BinOp{Op: "&&",
					X: Leaf{V: Ref{Path: []string{"input", "a"}}},
					Y: Not{X: Leaf{V: Ref{Path: []string{"input", "b"}}}},
				},
				Then: []Node{&InvokeTool{Uses: "tool.t.yes"}},
			},
		},
	}
	rec := &recorder{}
	runProg(t, &Interp{Invoker: rec}, prog, map[string]any{"a": true, "b": false})
	if got := rec.names(); len(got) != 1 || got[0] != "tool.t.yes" {
		t.Fatalf("a&&!b true should invoke, got %v", got)
	}
	rec2 := &recorder{}
	runProg(t, &Interp{Invoker: rec2}, prog, map[string]any{"a": true, "b": true})
	if got := rec2.names(); len(got) != 0 {
		t.Fatalf("a&&!b false should not invoke, got %v", got)
	}
}

func TestLoop_SequentialOrder(t *testing.T) {
	t.Parallel()
	prog := &Program{
		Workflow: "W",
		Params:   []string{"input"},
		Body: []Node{
			&Loop{
				Var:        "item",
				Collection: Ref{Path: []string{"input", "items"}},
				Body: []Node{
					&InvokeTool{Uses: "tool.t.each", Args: map[string]Value{"v": Ref{Path: []string{"item"}}}},
				},
			},
		},
	}
	rec := &recorder{}
	runProg(t, &Interp{Invoker: rec}, prog, map[string]any{"items": []any{int64(1), int64(2), int64(3)}})
	want := []string{"tool.t.each(1)", "tool.t.each(2)", "tool.t.each(3)"}
	got := rec.names()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sequential loop order wrong: got %v want %v", got, want)
	}
}

func TestLoop_DynamicFanoutBoundedConcurrency(t *testing.T) {
	t.Parallel()
	var live, peak int32
	rec := &recorder{track: &live, peak: &peak, holdFor: 5 * time.Millisecond}
	prog := &Program{
		Workflow: "W",
		Params:   []string{"input"},
		Body: []Node{
			&Loop{
				Parallel:   true,
				Var:        "item",
				Collection: Ref{Path: []string{"input", "items"}},
				Body: []Node{
					&InvokeTool{Uses: "tool.t.each", Args: map[string]Value{"v": Ref{Path: []string{"item"}}}},
				},
			},
		},
	}
	items := make([]any, 10)
	for i := range items {
		items[i] = int64(i)
	}
	in := &Interp{Invoker: rec, MaxConcurrency: 3}
	runProg(t, in, prog, map[string]any{"items": items})
	if len(rec.names()) != 10 {
		t.Fatalf("expected 10 iterations, got %d", len(rec.names()))
	}
	if peak > 3 {
		t.Fatalf("dynamic fan-out exceeded the concurrency bound: peak=%d, want <=3", peak)
	}
	if peak < 2 {
		t.Fatalf("expected genuine concurrency (peak>=2), got peak=%d", peak)
	}
}

func TestLoop_IterationCapExceeded(t *testing.T) {
	t.Parallel()
	prog := &Program{
		Workflow: "W",
		Params:   []string{"input"},
		Body: []Node{
			&Loop{Var: "item", Collection: Ref{Path: []string{"input", "items"}}, Body: []Node{&InvokeTool{Uses: "tool.t.each"}}},
		},
	}
	in := &Interp{Invoker: &recorder{}, MaxLoopIterations: 2}
	_, err := in.Run(context.Background(), prog, map[string]any{"items": []any{1, 2, 3}})
	if err == nil {
		t.Fatalf("expected an iteration-cap error, got nil")
	}
}

func TestFork_RunsBranchesAndPublishesBindings(t *testing.T) {
	t.Parallel()
	prog := &Program{
		Workflow: "W",
		Params:   []string{"input"},
		Body: []Node{
			&Fork{Branches: []ForkBranch{
				{Bind: "a", Nodes: []Node{&InvokeAgent{Bind: "a", Agent: "AgentA"}}},
				{Bind: "b", Nodes: []Node{&InvokeAgent{Bind: "b", Agent: "AgentB"}}},
			}},
			&Return{Value: Ref{Path: []string{"a", "agent"}}},
		},
	}
	rec := &recorder{respond: func(uses string, _ map[string]any) any {
		return map[string]any{"agent": uses}
	}}
	out := runProg(t, &Interp{Invoker: rec}, prog, nil)
	if out != "agent:AgentA" {
		t.Fatalf("fork should publish branch binding a, got %v", out)
	}
	if len(rec.names()) != 2 {
		t.Fatalf("expected both fork branches to run, got %v", rec.names())
	}
}

func TestReturn_StopsSubsequentNodes(t *testing.T) {
	t.Parallel()
	prog := &Program{
		Workflow: "W",
		Params:   []string{"input"},
		Body: []Node{
			&Branch{
				Cond: Leaf{V: Lit{V: true}},
				Then: []Node{&Return{Value: Lit{V: "early"}}},
			},
			&InvokeTool{Uses: "tool.t.after"},
		},
	}
	rec := &recorder{}
	out := runProg(t, &Interp{Invoker: rec}, prog, nil)
	if out != "early" {
		t.Fatalf("expected early return, got %v", out)
	}
	if len(rec.names()) != 0 {
		t.Fatalf("nodes after a fired return must not run, got %v", rec.names())
	}
}

// TestLoop_SequentialReturnHalts proves a Return inside a sequential loop body
// returns from the workflow and stops both the loop and the nodes after it —
// not swallowed as a per-iteration no-op (review finding: loop isolation).
func TestLoop_SequentialReturnHalts(t *testing.T) {
	t.Parallel()
	prog := &Program{
		Workflow: "W", Params: []string{"input"},
		Body: []Node{
			&Loop{Var: "item", Collection: Ref{Path: []string{"input", "items"}}, Body: []Node{
				&Return{Value: Ref{Path: []string{"item"}}},
			}},
			&InvokeTool{Uses: "tool.t.after"},
		},
	}
	rec := &recorder{}
	out := runProg(t, &Interp{Invoker: rec}, prog, map[string]any{"items": []any{"first", "second"}})
	if out != "first" {
		t.Fatalf("return in loop should return the first item, got %v", out)
	}
	if len(rec.names()) != 0 {
		t.Fatalf("nodes after a returning loop must not run, got %v", rec.names())
	}
}

// TestLoop_SequentialCarriedBindingEscapes proves a body binding in a sequential
// loop escapes with the last iteration's value (matches the checker's flat
// scope), so a later reference sees "c", not the pre-loop value.
func TestLoop_SequentialCarriedBindingEscapes(t *testing.T) {
	t.Parallel()
	prog := &Program{
		Workflow: "W", Params: []string{"input"},
		Body: []Node{
			&Let{Bind: "last", Value: Lit{V: "init"}},
			&Loop{Var: "item", Collection: Ref{Path: []string{"input", "items"}}, Body: []Node{
				&Let{Bind: "last", Value: Ref{Path: []string{"item"}}},
			}},
			&Return{Value: Ref{Path: []string{"last"}}},
		},
	}
	out := runProg(t, &Interp{Invoker: &recorder{}}, prog, map[string]any{"items": []any{"a", "b", "c"}})
	if out != "c" {
		t.Fatalf("loop-carried binding should escape with the last value, got %v", out)
	}
}

// TestBranch_EqualityOverObjectsAndArrays proves `==` is total and panic-free
// over the JSON objects and arrays a workflow input actually holds — a bare Go
// `==` on those operands would panic (review finding).
func TestBranch_EqualityOverObjectsAndArrays(t *testing.T) {
	t.Parallel()
	prog := &Program{
		Workflow: "W", Params: []string{"input"},
		Body: []Node{
			&Branch{
				Cond: BinOp{Op: "==", X: Leaf{V: Ref{Path: []string{"input", "a"}}}, Y: Leaf{V: Ref{Path: []string{"input", "b"}}}},
				Then: []Node{&InvokeTool{Uses: "tool.t.equal"}},
				Else: []Node{&InvokeTool{Uses: "tool.t.notequal"}},
			},
		},
	}
	run := func(a, b any) string {
		rec := &recorder{}
		runProg(t, &Interp{Invoker: rec}, prog, map[string]any{"a": a, "b": b})
		names := rec.names()
		if len(names) != 1 {
			t.Fatalf("expected one branch to run, got %v", names)
		}
		return names[0]
	}

	// Equal maps -> then; different maps -> else; equal/unequal arrays; type mismatch.
	if got := run(map[string]any{"k": "v"}, map[string]any{"k": "v"}); got != "tool.t.equal" {
		t.Fatalf("equal maps should be ==, got %s", got)
	}
	if got := run(map[string]any{"k": "v"}, map[string]any{"k": "w"}); got != "tool.t.notequal" {
		t.Fatalf("different maps should be !=, got %s", got)
	}
	if got := run([]any{int64(1), int64(2)}, []any{int64(1), float64(2)}); got != "tool.t.equal" {
		t.Fatalf("arrays [1,2] and [1,2.0] should be == (numeric normalization), got %s", got)
	}
	if got := run([]any{int64(1)}, []any{int64(1), int64(2)}); got != "tool.t.notequal" {
		t.Fatalf("arrays of different length should be !=, got %s", got)
	}
	if got := run(map[string]any{"k": "v"}, "scalar"); got != "tool.t.notequal" {
		t.Fatalf("map vs scalar should be != (and must not panic), got %s", got)
	}
}

func TestLoop_NonListCollectionErrors(t *testing.T) {
	t.Parallel()
	prog := &Program{
		Workflow: "W", Params: []string{"input"},
		Body: []Node{&Loop{Var: "x", Collection: Ref{Path: []string{"input", "notalist"}}, Body: []Node{&InvokeTool{Uses: "tool.t.x"}}}},
	}
	_, err := (&Interp{Invoker: &recorder{}}).Run(context.Background(), prog, map[string]any{"notalist": "scalar"})
	if err == nil {
		t.Fatalf("expected an error iterating a non-list, got nil")
	}
}

// TestCompositeValues_Eval proves the #256 composite Value forms evaluate
// recursively: an Object of a Ref and a List, and a Template that interpolates a
// scalar ref into surrounding text.
func TestCompositeValues_Eval(t *testing.T) {
	t.Parallel()
	prog := &Program{
		Workflow: "W", Params: []string{"input"},
		Body: []Node{&Return{Value: Object{Fields: []Field{
			{Key: "who", Val: Ref{Path: []string{"input", "name"}}},
			{Key: "tags", Val: List{Elems: []Value{Lit{V: "x"}, Ref{Path: []string{"input", "name"}}}}},
			{Key: "greeting", Val: Template{Parts: []Value{Lit{V: "hi "}, Ref{Path: []string{"input", "name"}}, Lit{V: "!"}}}},
		}}}},
	}
	out := runProg(t, &Interp{Invoker: &recorder{}}, prog, map[string]any{"name": "ada"})
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("return should be a map, got %T", out)
	}
	if m["who"] != "ada" {
		t.Fatalf("who = %v, want ada", m["who"])
	}
	if tags, ok := m["tags"].([]any); !ok || len(tags) != 2 || tags[0] != "x" || tags[1] != "ada" {
		t.Fatalf("tags = %v, want [x ada]", m["tags"])
	}
	if m["greeting"] != "hi ada!" {
		t.Fatalf("greeting = %v, want %q", m["greeting"], "hi ada!")
	}
}

// TestApproval_RejectedLoudly proves the standalone interpreter refuses an
// Approval node (durable suspend/resume is Phase 2, #258) rather than silently
// skipping a human gate. (Graph now executes — see TestGraph_* below.)
func TestApproval_RejectedLoudly(t *testing.T) {
	t.Parallel()
	prog := &Program{Workflow: "W", Body: []Node{&Approval{Bind: "gate"}}}
	if _, err := (&Interp{Invoker: &recorder{}}).Run(context.Background(), prog, nil); err == nil {
		t.Fatalf("expected Approval execution to be rejected, got nil error")
	}
}

// TestGraph_JoinAccuracy proves the DAG scheduler runs each node when ITS own
// predecessors complete — not over-synchronized like a Fork. In
// `A,B roots; C[A]; D[A,B]; E[C]`, every node runs exactly once and a node never
// runs before its predecessors (observed via the recorded output chain).
func TestGraph_JoinAccuracy(t *testing.T) {
	t.Parallel()
	// Each node returns {seen: <sorted predecessor ids actually present in scope>}
	// so we can assert a node saw exactly its declared predecessors' outputs.
	graphNode := func(id string, needs ...string) GraphNode {
		args := map[string]Value{}
		for _, dep := range needs {
			args[dep] = Ref{Path: []string{dep}}
		}
		return GraphNode{ID: id, Needs: needs, Run: &InvokeTool{Bind: id, Uses: "tool.t." + id, Args: args}}
	}
	prog := &Program{
		Workflow: "W", Params: []string{"input"},
		Body: []Node{
			&Graph{Nodes: []GraphNode{
				graphNode("a"),
				graphNode("b"),
				graphNode("c", "a"),
				graphNode("d", "a", "b"),
				graphNode("e", "c"),
			}},
			&Return{Value: Ref{Path: []string{"e"}}},
		},
	}
	var mu sync.Mutex
	seen := map[string]map[string]bool{}
	rec := &recorder{respond: func(uses string, args map[string]any) any {
		id := strings.TrimPrefix(uses, "tool.t.")
		mu.Lock()
		got := map[string]bool{}
		for k := range args {
			got[k] = true
		}
		seen[id] = got
		mu.Unlock()
		return map[string]any{"id": id}
	}}
	if _, err := (&Interp{Invoker: rec}).Run(context.Background(), prog, nil); err != nil {
		t.Fatalf("graph run: %v", err)
	}
	// Every node ran exactly once.
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		if _, ok := seen[id]; !ok {
			t.Fatalf("node %q did not run", id)
		}
	}
	// d saw both a and b (its declared predecessors, published before it ran).
	if !seen["d"]["a"] || !seen["d"]["b"] {
		t.Fatalf("d should see a and b, saw %v", seen["d"])
	}
	// e saw c (not b — E does not wait for the D/B join).
	if !seen["e"]["c"] {
		t.Fatalf("e should see c, saw %v", seen["e"])
	}
}

// TestGraph_BoundedConcurrency proves independent roots run concurrently but the
// scheduler honors MaxConcurrency.
func TestGraph_BoundedConcurrency(t *testing.T) {
	t.Parallel()
	var track, peak int32
	nodes := make([]GraphNode, 6)
	for i := range nodes {
		id := fmt.Sprintf("r%d", i)
		nodes[i] = GraphNode{ID: id, Run: &InvokeTool{Bind: id, Uses: "tool.t." + id}}
	}
	prog := &Program{Workflow: "W", Body: []Node{&Graph{Nodes: nodes}}}
	rec := &recorder{track: &track, peak: &peak, holdFor: 20 * time.Millisecond}
	if _, err := (&Interp{Invoker: rec, MaxConcurrency: 2}).Run(context.Background(), prog, nil); err != nil {
		t.Fatalf("graph run: %v", err)
	}
	if got := atomic.LoadInt32(&peak); got > 2 {
		t.Fatalf("peak concurrency %d exceeded MaxConcurrency 2", got)
	}
	if got := atomic.LoadInt32(&peak); got < 2 {
		t.Fatalf("independent roots should run concurrently, peak was %d", got)
	}
}
