package check

import (
	"testing"

	"github.com/Terfyn/terfyn/internal/execir"
)

// collectInvokeWorkflowArgs walks an execir program body (through every control-flow construct) and
// returns each InvokeWorkflow's argument keys, keyed by its bind name.
func collectInvokeWorkflowArgs(nodes []execir.Node, out map[string][]string) {
	keys := func(m map[string]execir.Value) []string {
		ks := make([]string, 0, len(m))
		for k := range m {
			ks = append(ks, k)
		}
		return ks
	}
	for _, n := range nodes {
		switch v := n.(type) {
		case *execir.InvokeWorkflow:
			out[v.Bind] = keys(v.Args)
		case *execir.Branch:
			collectInvokeWorkflowArgs(v.Then, out)
			collectInvokeWorkflowArgs(v.Else, out)
		case *execir.Loop:
			collectInvokeWorkflowArgs(v.Body, out)
		case *execir.While:
			collectInvokeWorkflowArgs(v.Body, out)
		case *execir.Retry:
			collectInvokeWorkflowArgs(v.Body, out)
		case *execir.Fork:
			for i := range v.Branches {
				collectInvokeWorkflowArgs(v.Branches[i].Nodes, out)
			}
		case *execir.Graph:
			for i := range v.Nodes {
				collectInvokeWorkflowArgs([]execir.Node{v.Nodes[i].Run}, out)
			}
		}
	}
}

// TestCheck_PositionalWorkflowArgsRebindInEveryConstruct is the regression for issue #379: a
// positional workflow call must be rebound to the callee's parameter name (x) in the execution IR
// regardless of which control-flow construct encloses it. Before the fix, a call inside `while` or
// `retry` kept the lowering placeholder `arg0`, so the callee received {"arg0": …} at run time (schema
// failure / nil reads) while the resource projection deceptively showed `x`.
func TestCheck_PositionalWorkflowArgsRebindInEveryConstruct(t *testing.T) {
	t.Parallel()
	src := `
workflow inner(x: any) { return x }
workflow outer(input: any) {
    a = inner("plain")
    if input.flag == 2 { d = inner("in-if") }
    for r in input.repos { e = inner("in-for") }
    parallel for item in input.items { f = inner("in-parallel") }
    while input.flag == 1 limit 2 { b = inner("in-while") }
    retry until input.ok == 1 limit 2 { c = inner("in-retry") }
    return a
}
`
	f := parseOrFatal(t, src)
	prog, diags := Check(f, Options{})
	if diags.HasErrors() {
		t.Fatalf("program must check clean: %v", diagMessages(diags))
	}
	outer := prog.Executables["outer"]
	if outer == nil {
		t.Fatalf("no executable for outer in %v", prog.Executables)
	}

	got := map[string][]string{}
	collectInvokeWorkflowArgs(outer.Body, got)

	// Every construct's call must carry the callee's parameter name, never the placeholder.
	for _, bind := range []string{"a", "b", "c", "d", "e", "f"} {
		args, ok := got[bind]
		if !ok {
			t.Fatalf("no InvokeWorkflow bound to %q; collected %v", bind, got)
		}
		if len(args) != 1 || args[0] != "x" {
			t.Fatalf("call %q args = %v, want [x] (issue #379: positional arg not rebound)", bind, args)
		}
	}
}
