package execir

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
		case *Branch, *Loop:
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

// RequiresInterpreterTransitive reports whether the workflow named root, OR any
// workflow reachable from it through an InvokeWorkflow (`workflow:`) edge,
// requires the interpreter (issue #259). The whole run must route to execir when
// this is true: a control-flow workflow reached as a CALLEE must not be run on
// the flattening DAG either — the execir InvokeWorkflow path (#270) executes a
// nested control-flow child correctly, and routing the entry to execir reuses it.
// programs is keyed by workflow name (ResolvedConfig.Executables / a snapshot's
// pinned programs).
func RequiresInterpreterTransitive(programs map[string]*Program, root string) bool {
	seen := make(map[string]bool)
	var visit func(name string) bool
	visit = func(name string) bool {
		if seen[name] {
			return false
		}
		seen[name] = true
		p := programs[name]
		if p == nil {
			return false
		}
		if nodesRequireInterpreter(p.Body) {
			return true
		}
		for _, callee := range invokeWorkflowCallees(p.Body) {
			if visit(callee) {
				return true
			}
		}
		return false
	}
	return visit(root)
}

func invokeWorkflowCallees(nodes []Node) []string {
	var out []string
	for _, n := range nodes {
		switch v := n.(type) {
		case *InvokeWorkflow:
			out = append(out, v.Workflow)
		case *Branch:
			out = append(out, invokeWorkflowCallees(v.Then)...)
			out = append(out, invokeWorkflowCallees(v.Else)...)
		case *Loop:
			out = append(out, invokeWorkflowCallees(v.Body)...)
		case *Fork:
			for _, b := range v.Branches {
				out = append(out, invokeWorkflowCallees(b.Nodes)...)
			}
		case *Graph:
			for _, gn := range v.Nodes {
				if gn.Run != nil {
					out = append(out, invokeWorkflowCallees([]Node{gn.Run})...)
				}
			}
		}
	}
	return out
}
