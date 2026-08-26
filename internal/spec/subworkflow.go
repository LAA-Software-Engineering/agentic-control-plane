package spec

import (
	"sort"
	"strings"
)

// validateSubworkflowGraph rejects recursion (direct and indirect) and enforces the
// nesting-depth bound on `workflow:` steps (issue #194). It operates over the graph-global
// "workflow invokes workflow" adjacency built from [RefIndex.WorkflowSubs]. Edges to workflows
// that do not exist are ignored here (reported as missing references by the caller).
//
// Recursion is rejected before depth is measured: a cyclic call graph has no finite nesting
// depth, so the depth walk only runs once the graph is proven acyclic.
func validateSubworkflowGraph(g *ProjectGraph, ix *RefIndex) []error {
	if g == nil || ix == nil {
		return nil
	}
	names := make([]string, 0, len(g.Workflows))
	for name, wr := range g.Workflows {
		if wr == nil {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	// adj holds only edges to workflows that exist, deduped and deterministically ordered.
	adj := make(map[string][]string, len(names))
	exists := make(map[string]struct{}, len(names))
	for _, n := range names {
		exists[n] = struct{}{}
	}
	for _, caller := range names {
		var callees []string
		for _, callee := range ix.WorkflowSubs[caller] {
			callee = strings.TrimSpace(callee)
			if callee == "" {
				continue
			}
			if _, ok := exists[callee]; !ok {
				continue
			}
			callees = append(callees, callee)
		}
		adj[caller] = callees
	}

	if errs := subworkflowCycleErrors(g, names, adj); len(errs) > 0 {
		return errs
	}
	return subworkflowDepthErrors(g, names, adj)
}

// subworkflowCycleErrors reports at most one recursion cycle, anchored at the step whose
// `workflow:` edge closes the cycle, with a readable path (issue #194 acceptance: positions).
func subworkflowCycleErrors(g *ProjectGraph, names []string, adj map[string][]string) []error {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(names))
	var stack []string
	var cycle []string
	var closeFrom, closeTo string

	var dfs func(string) bool
	dfs = func(u string) bool {
		color[u] = gray
		stack = append(stack, u)
		for _, v := range adj[u] {
			switch color[v] {
			case gray:
				closeFrom, closeTo = u, v
				// Extract the path from v (start of cycle) to u.
				start := 0
				for i, s := range stack {
					if s == v {
						start = i
						break
					}
				}
				cycle = append(append([]string(nil), stack[start:]...), v)
				return true
			case white:
				if dfs(v) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[u] = black
		return false
	}

	for _, n := range names {
		if color[n] == white && dfs(n) {
			pos := workflowSubPos(g.Workflows[closeFrom], closeTo)
			return []error{pos.Errorf(
				"workflow %s: subworkflow recursion via %q: %s",
				closeFrom, closeTo, strings.Join(cycle, " -> "),
			)}
		}
	}
	return nil
}

// subworkflowDepthErrors reports the single deepest chain when it exceeds
// [DefaultMaxSubworkflowDepth]. Depth is the number of nested `workflow:` edges: a workflow
// that calls no subworkflow has depth 0. The graph is acyclic when this runs.
func subworkflowDepthErrors(g *ProjectGraph, names []string, adj map[string][]string) []error {
	depth := make(map[string]int, len(names))
	next := make(map[string]string, len(names))

	var walk func(string) int
	walk = func(u string) int {
		if d, ok := depth[u]; ok {
			return d
		}
		best := 0
		bestNext := ""
		for _, v := range adj[u] {
			if d := walk(v) + 1; d > best {
				best = d
				bestNext = v
			}
		}
		depth[u] = best
		next[u] = bestNext
		return best
	}

	worstRoot := ""
	worst := 0
	for _, n := range names {
		if d := walk(n); d > worst {
			worst = d
			worstRoot = n
		}
	}
	if worst <= DefaultMaxSubworkflowDepth {
		return nil
	}

	// Build the witnessing chain and anchor the error at its first `workflow:` step.
	chain := []string{worstRoot}
	for cur := worstRoot; next[cur] != ""; cur = next[cur] {
		chain = append(chain, next[cur])
	}
	pos := g.Workflows[worstRoot].Pos
	if len(chain) > 1 {
		pos = workflowSubPos(g.Workflows[worstRoot], chain[1])
	}
	return []error{pos.Errorf(
		"workflow %s: subworkflow nesting depth %d exceeds limit %d: %s",
		worstRoot, worst, DefaultMaxSubworkflowDepth, strings.Join(chain, " -> "),
	)}
}
