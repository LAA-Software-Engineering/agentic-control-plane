package execir

import "math"

// RequiresInterpreter reports whether a program must run on the execir
// interpreter rather than the resource-projection DAG (issue #259). It is true
// when the program contains a Branch or a Loop anywhere: those are the constructs
// the resource projection cannot represent faithfully — it FLATTENS both arms of
// a conditional and a loop body into steps (a sound effect over-approximation,
// an unsound execution), so running them on the DAG would execute every arm.
//
// Graph (a YAML needs-DAG), Fork (`.agent parallel { }`), and straight-line node
// lists ARE faithfully represented by the resource projection, so a program built
// only from those stays on the DAG (no behavior change for existing workflows).
func RequiresInterpreter(p *Program) bool {
	if p == nil {
		return false
	}
	return nodesRequireInterpreter(p.Body)
}

func nodesRequireInterpreter(nodes []Node) bool {
	for _, n := range nodes {
		switch v := n.(type) {
		case *Branch, *Loop, *While:
			return true
		case *Fork:
			for _, b := range v.Branches {
				if nodesRequireInterpreter(b.Nodes) {
					return true
				}
			}
		case *Graph:
			for _, gn := range v.Nodes {
				if gn.Run != nil && nodesRequireInterpreter([]Node{gn.Run}) {
					return true
				}
			}
		}
	}
	return false
}

// InvocationBound is an upper bound on how many times one callee (an agent, a tool
// operation, or a subworkflow) may be invoked in a single run of a workflow, derived
// from the enclosing bounded loops (issue #293). Kind is "agent" | "tool" |
// "workflow"; Callee is the agent name, the tool uses string, or the workflow name.
// DataBounded is true when a `for` over a runtime collection is on the path: the
// count is then not decidable from source alone, and Max falls back to the global
// loop cap as a conservative ceiling.
type InvocationBound struct {
	Kind        string
	Callee      string
	Max         int
	DataBounded bool
}

type invAcc struct {
	max         int
	dataBounded bool
}

// InvocationBounds computes the per-callee invocation upper bounds for a program.
// A bounded `while limit N` multiplies the counts of its body by N; a `for` over a
// runtime collection multiplies by globalLoopCap and marks the result data-bounded;
// a `Branch` takes the MAX over its arms (only one runs); a `Fork` SUMS its branches
// (all run). The result is a sound upper bound — never an undercount — over the
// callees directly invoked by the program (subworkflow bodies are not expanded
// transitively; an InvokeWorkflow counts as one workflow invocation). Results are
// sorted by kind then callee for deterministic output.
func InvocationBounds(p *Program, globalLoopCap int) []InvocationBound {
	if p == nil {
		return nil
	}
	if globalLoopCap <= 0 {
		globalLoopCap = 1
	}
	acc := boundsOf(p.Body, 1, false, globalLoopCap)
	out := make([]InvocationBound, 0, len(acc))
	for key, v := range acc {
		kind, callee := key[0], key[1]
		out = append(out, InvocationBound{Kind: kind, Callee: callee, Max: v.max, DataBounded: v.dataBounded})
	}
	sortInvocationBounds(out)
	return out
}

type calleeKey [2]string

func boundsOf(nodes []Node, factor int, dataBounded bool, cap int) map[calleeKey]invAcc {
	acc := map[calleeKey]invAcc{}
	add := func(kind, callee string) {
		if callee == "" {
			return
		}
		k := calleeKey{kind, callee}
		cur := acc[k]
		cur.max = satAdd(cur.max, factor)
		cur.dataBounded = cur.dataBounded || dataBounded
		acc[k] = cur
	}
	mergeSum := func(other map[calleeKey]invAcc) {
		for k, v := range other {
			cur := acc[k]
			cur.max = satAdd(cur.max, v.max)
			cur.dataBounded = cur.dataBounded || v.dataBounded
			acc[k] = cur
		}
	}
	for _, n := range nodes {
		switch v := n.(type) {
		case *InvokeAgent:
			add("agent", v.Agent)
		case *InvokeTool:
			add("tool", v.Uses)
		case *InvokeWorkflow:
			add("workflow", v.Workflow)
		case *While:
			// The runtime caps a `while` at min(Limit, MaxLoopIterations) (#299), so
			// multiplying that tight factor — not the raw source Limit — keeps the
			// bound honest (matching what can actually run) AND overflow-resistant
			// (each factor is ≤ cap). satMul saturates as a backstop for deep nesting.
			lim := v.Limit
			if lim <= 0 || lim > cap {
				lim = cap
			}
			mergeSum(boundsOf(v.Body, satMul(factor, lim), dataBounded, cap))
		case *Loop:
			// A for/parallel-for over a runtime collection is not statically bounded;
			// the global loop cap is the conservative ceiling.
			mergeSum(boundsOf(v.Body, satMul(factor, cap), true, cap))
		case *Branch:
			// Only one arm runs: the bound is the MAX over arms, per callee.
			mergeMaxInto(acc, boundsOf(v.Then, factor, dataBounded, cap), boundsOf(v.Else, factor, dataBounded, cap))
		case *Fork:
			for _, br := range v.Branches {
				mergeSum(boundsOf(br.Nodes, factor, dataBounded, cap))
			}
		case *Graph:
			for _, gn := range v.Nodes {
				if gn.Run != nil {
					mergeSum(boundsOf([]Node{gn.Run}, factor, dataBounded, cap))
				}
			}
		}
	}
	return acc
}

// mergeMaxInto merges the two branch-arm maps into dst by taking the per-callee MAX
// (only one arm runs), then adds that into dst (sequential composition with what
// preceded the Branch).
func mergeMaxInto(dst map[calleeKey]invAcc, then, els map[calleeKey]invAcc) {
	maxed := map[calleeKey]invAcc{}
	for k, v := range then {
		maxed[k] = v
	}
	for k, v := range els {
		cur, ok := maxed[k]
		if !ok || v.max > cur.max {
			cur.max = maxIntBound(cur.max, v.max)
		}
		cur.dataBounded = cur.dataBounded || v.dataBounded
		maxed[k] = cur
	}
	for k, v := range maxed {
		cur := dst[k]
		cur.max = satAdd(cur.max, v.max)
		cur.dataBounded = cur.dataBounded || v.dataBounded
		dst[k] = cur
	}
}

func maxIntBound(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// satMul and satAdd are saturating (never-wrapping) arithmetic over the non-negative
// invocation counts: on overflow they clamp to math.MaxInt rather than wrapping to a
// negative bound, so the computed upper bound is never an undercount (#293 review).
// Overflow is already unreachable for realistic workflows — each loop factor is
// min(Limit, cap) ≤ cap, so it takes deep nesting to approach the limit — this is the
// backstop.
func satMul(a, b int) int {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a > math.MaxInt/b {
		return math.MaxInt
	}
	return a * b
}

func satAdd(a, b int) int {
	if b > 0 && a > math.MaxInt-b {
		return math.MaxInt
	}
	return a + b
}

func sortInvocationBounds(b []InvocationBound) {
	for i := 1; i < len(b); i++ {
		for j := i; j > 0 && lessInvocation(b[j], b[j-1]); j-- {
			b[j], b[j-1] = b[j-1], b[j]
		}
	}
}

func lessInvocation(a, b InvocationBound) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.Callee < b.Callee
}
