package spec

import (
	"strings"
)

type workflowCallEdge struct {
	from, to string
	step     WorkflowStep
}

// validateWorkflowCalls rejects dangling is already handled as MissingRefError.
// This pass rejects direct/mutual recursion and nesting deeper than maxWorkflowNesting
// (issue #194). Positions point at the workflow: field of the offending step.
func validateWorkflowCalls(g *ProjectGraph) []error {
	if g == nil {
		return nil
	}
	var edges []workflowCallEdge
	adj := map[string][]workflowCallEdge{}
	for from, wr := range g.Workflows {
		if wr == nil {
			continue
		}
		for _, st := range wr.Spec.Steps {
			to := strings.TrimSpace(st.Workflow)
			if to == "" {
				continue
			}
			if _, ok := g.Workflows[to]; !ok {
				continue
			}
			e := workflowCallEdge{from: from, to: to, step: st}
			edges = append(edges, e)
			adj[from] = append(adj[from], e)
		}
	}
	if len(edges) == 0 {
		return nil
	}

	var errs []error
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var cycleEdge workflowCallEdge
	found := false
	var dfs func(string) bool
	dfs = func(u string) bool {
		color[u] = gray
		for _, e := range adj[u] {
			switch color[e.to] {
			case gray:
				cycleEdge = e
				return true
			case white:
				if dfs(e.to) {
					return true
				}
			}
		}
		color[u] = black
		return false
	}
	for name := range g.Workflows {
		if color[name] == white && dfs(name) {
			found = true
			break
		}
	}
	if found {
		pos := cycleEdge.step.WorkflowPos
		if pos.IsZero() {
			pos = cycleEdge.step.Pos
		}
		path := workflowCallCyclePath(adj, cycleEdge.from, cycleEdge.to)
		errs = append(errs, pos.Errorf(
			"workflow %s step %q: workflow call cycle involving %s",
			cycleEdge.from, strings.TrimSpace(cycleEdge.step.ID), path,
		))
	}

	errs = append(errs, validateWorkflowNestingDepth(g, adj)...)
	return errs
}

func validateWorkflowNestingDepth(g *ProjectGraph, adj map[string][]workflowCallEdge) []error {
	var errs []error
	for entry, wr := range g.Workflows {
		if wr == nil {
			continue
		}
		max := ResolveMaxWorkflowNesting(&g.Spec, &wr.Spec)
		type frame struct {
			name  string
			depth int
		}
		stack := []frame{{name: entry, depth: 0}}
		seen := map[string]int{}
		for len(stack) > 0 {
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if prev, ok := seen[f.name]; ok && prev >= f.depth {
				continue
			}
			seen[f.name] = f.depth
			for _, e := range adj[f.name] {
				d := f.depth + 1
				if d > max {
					pos := e.step.WorkflowPos
					if pos.IsZero() {
						pos = e.step.Pos
					}
					errs = append(errs, pos.Errorf(
						"workflow %s step %q: workflow nesting depth %d exceeds maxWorkflowNesting %d (path %s -> %s)",
						e.from, strings.TrimSpace(e.step.ID), d, max, nestingPath(entry, e.from), e.to,
					))
					continue
				}
				stack = append(stack, frame{name: e.to, depth: d})
			}
		}
	}
	return errs
}

func nestingPath(from, to string) string {
	if from == to {
		return from
	}
	return from + " -> " + to
}

func workflowCallCyclePath(adj map[string][]workflowCallEdge, from, to string) string {
	ids := []string{from, to}
	seen := map[string]struct{}{from: {}, to: {}}
	cur := to
	for i := 0; i < len(adj)+2; i++ {
		if cur == from {
			break
		}
		next := ""
		for _, e := range adj[cur] {
			if e.to == from {
				next = e.to
				break
			}
			if _, ok := seen[e.to]; !ok {
				next = e.to
			}
		}
		if next == "" {
			break
		}
		ids = append(ids, next)
		if next == from {
			break
		}
		seen[next] = struct{}{}
		cur = next
	}
	return strings.Join(ids, " -> ")
}

// WorkflowCallPos is the diagnostic location of a workflow: field.
func WorkflowCallPos(st WorkflowStep) Pos {
	if !st.WorkflowPos.IsZero() {
		return st.WorkflowPos
	}
	return st.Pos
}
