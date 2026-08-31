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
