package execir

import (
	"context"
	"fmt"
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

func (r *recorder) InvokeTool(_ context.Context, uses string, args map[string]any) (any, error) {
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

func (r *recorder) InvokeAgent(_ context.Context, agent string, args map[string]any) (any, error) {
	r.mu.Lock()
	r.tools = append(r.tools, "agent:"+agent)
	r.mu.Unlock()
	if r.respond != nil {
		return r.respond("agent:"+agent, args), nil
	}
	return map[string]any{"agent": agent}, nil
}

func (r *recorder) InvokeWorkflow(_ context.Context, wf string, args map[string]any) (any, error) {
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
