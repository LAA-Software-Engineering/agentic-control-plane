package lower

import (
	"context"
	"sync"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/execir"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/lang"
)

// endToEndInvoker records tool calls and returns a per-uses canned result.
type endToEndInvoker struct {
	mu    sync.Mutex
	calls []string
}

func (e *endToEndInvoker) InvokeTool(_ context.Context, uses string, args map[string]any) (any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	label := uses
	if v, ok := args["v"]; ok {
		label += ":"
		if s, ok := v.(string); ok {
			label += s
		}
	}
	e.calls = append(e.calls, label)
	return map[string]any{"ok": true}, nil
}
func (e *endToEndInvoker) InvokeAgent(_ context.Context, a string, _ map[string]any) (any, error) {
	return map[string]any{"agent": a}, nil
}
func (e *endToEndInvoker) InvokeWorkflow(_ context.Context, w string, _ map[string]any) (any, error) {
	return nil, nil
}

// TestExec_ParseLowerExecute is the primary acceptance path: a control-flow
// .agent workflow parses, lowers to the execution IR, and executes — the
// conditional selects an arm and the loop iterates the runtime collection.
func TestExec_ParseLowerExecute(t *testing.T) {
	t.Parallel()
	prog, diags := lowerExecOrFatal(t, `
workflow W(input: Job) {
    if input.enabled {
        github.enable()
    } else {
        github.disable()
    }
    for name in input.names {
        github.touch(v: name)
    }
    return input.enabled
}
`, nil)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	inv := &endToEndInvoker{}
	interp := &execir.Interp{Invoker: inv}
	out, err := interp.Run(context.Background(), prog, map[string]any{
		"enabled": true,
		"names":   []any{"a", "b"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != true {
		t.Fatalf("expected return true, got %v", out)
	}
	want := []string{"tool.github.enable", "tool.github.touch:a", "tool.github.touch:b"}
	if len(inv.calls) != len(want) {
		t.Fatalf("call sequence mismatch: got %v want %v", inv.calls, want)
	}
	for i := range want {
		if inv.calls[i] != want[i] {
			t.Fatalf("call[%d]=%q want %q (all: %v)", i, inv.calls[i], want[i], inv.calls)
		}
	}
}

func lowerExecOrFatal(t *testing.T, src string, workflows map[string]bool) (*execir.Program, lang.Diagnostics) {
	t.Helper()
	f, diags := lang.Parse("test.agent", src)
	if diags.HasErrors() {
		t.Fatalf("parse errors: %v", diags)
	}
	if len(f.Decls) == 0 {
		t.Fatalf("no declarations parsed")
	}
	wd, ok := f.Decls[0].(*lang.WorkflowDecl)
	if !ok {
		t.Fatalf("first decl is not a workflow")
	}
	return LowerExec(wd, workflows)
}

func TestLowerExec_IfLowersToBranch(t *testing.T) {
	t.Parallel()
	prog, diags := lowerExecOrFatal(t, `
workflow W(input: PR) {
    if input.urgent {
        github.notify()
    } else {
        github.skip()
    }
}
`, nil)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(prog.Body) != 1 {
		t.Fatalf("expected 1 top node, got %d", len(prog.Body))
	}
	br, ok := prog.Body[0].(*execir.Branch)
	if !ok {
		t.Fatalf("expected Branch, got %T", prog.Body[0])
	}
	if _, ok := br.Cond.(execir.Leaf); !ok {
		t.Fatalf("expected a Leaf condition, got %T", br.Cond)
	}
	if len(br.Then) != 1 || len(br.Else) != 1 {
		t.Fatalf("expected one node per arm, got then=%d else=%d", len(br.Then), len(br.Else))
	}
	if it, ok := br.Then[0].(*execir.InvokeTool); !ok || it.Uses != "tool.github.notify" {
		t.Fatalf("then arm should invoke tool.github.notify, got %#v", br.Then[0])
	}
}

func TestLowerExec_ForAndParallelFor(t *testing.T) {
	t.Parallel()
	prog, diags := lowerExecOrFatal(t, `
workflow W(input: Repos) {
    for repo in input.repos {
        github.build(repo)
    }
    parallel for item in input.items {
        github.scan(item)
    }
}
`, nil)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(prog.Body) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(prog.Body))
	}
	seq, ok := prog.Body[0].(*execir.Loop)
	if !ok || seq.Parallel {
		t.Fatalf("first node should be a sequential Loop, got %#v", prog.Body[0])
	}
	if seq.Var != "repo" {
		t.Fatalf("loop var should be repo, got %q", seq.Var)
	}
	par, ok := prog.Body[1].(*execir.Loop)
	if !ok || !par.Parallel {
		t.Fatalf("second node should be a parallel Loop, got %#v", prog.Body[1])
	}
}

func TestLowerExec_ParallelLowersToFork(t *testing.T) {
	t.Parallel()
	prog, diags := lowerExecOrFatal(t, `
workflow W(input: PR) {
    parallel {
        sec = Security(input)
        qual = Quality(input)
    }
    return sec
}
`, nil)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	fork, ok := prog.Body[0].(*execir.Fork)
	if !ok {
		t.Fatalf("expected Fork, got %T", prog.Body[0])
	}
	if len(fork.Branches) != 2 {
		t.Fatalf("expected 2 fork branches, got %d", len(fork.Branches))
	}
	if fork.Branches[0].Bind != "sec" {
		t.Fatalf("first branch should bind sec, got %q", fork.Branches[0].Bind)
	}
}

func TestLowerExec_NestedCallHoisted(t *testing.T) {
	t.Parallel()
	// A nested call argument must be hoisted into its own preceding Invoke and
	// referenced by a temporary binding.
	prog, diags := lowerExecOrFatal(t, `
workflow W(input: PR) {
    result = Synth(github.get_pr(input.repo))
}
`, nil)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(prog.Body) != 2 {
		t.Fatalf("expected the nested call to be hoisted (2 nodes), got %d", len(prog.Body))
	}
	inner, ok := prog.Body[0].(*execir.InvokeTool)
	if !ok || inner.Uses != "tool.github.get_pr" {
		t.Fatalf("first node should be the hoisted tool call, got %#v", prog.Body[0])
	}
	outer, ok := prog.Body[1].(*execir.InvokeAgent)
	if !ok || outer.Bind != "result" {
		t.Fatalf("second node should bind result via the agent, got %#v", prog.Body[1])
	}
	ref, ok := outer.Args["arg0"].(execir.Ref)
	if !ok || len(ref.Path) != 1 || ref.Path[0] != inner.Bind {
		t.Fatalf("outer call should reference the hoisted temp %q, got %#v", inner.Bind, outer.Args["arg0"])
	}
}

func TestLowerExec_WorkflowCalleeClassification(t *testing.T) {
	t.Parallel()
	prog, diags := lowerExecOrFatal(t, `
workflow W(input: PR) {
    x = Sub(input)
}
`, map[string]bool{"Sub": true})
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if _, ok := prog.Body[0].(*execir.InvokeWorkflow); !ok {
		t.Fatalf("Sub should classify as InvokeWorkflow, got %T", prog.Body[0])
	}
}

func TestLowerExec_CallInConditionDiagnosed(t *testing.T) {
	t.Parallel()
	_, diags := lowerExecOrFatal(t, `
workflow W(input: PR) {
    if github.check(input) {
        github.act()
    }
}
`, nil)
	if !diags.HasErrors() {
		t.Fatalf("expected a call-in-condition diagnostic")
	}
}
