package spec

import (
	"strings"
)

// WorkflowUsesExplicitNeeds reports whether any step opts the workflow into graph mode
// (issue #192). A workflow with no `needs:` keys keeps implicit sequential semantics:
// YAML order is an implicit chain (step i waits for step i-1).
func WorkflowUsesExplicitNeeds(steps []WorkflowStep) bool {
	for _, st := range steps {
		if st.NeedsDeclared || len(st.Needs) > 0 {
			return true
		}
	}
	return false
}

// StepNeedsIDs returns the declared or implicit predecessor IDs for steps[i].
func StepNeedsIDs(steps []WorkflowStep, i int) []string {
	if i < 0 || i >= len(steps) {
		return nil
	}
	if WorkflowUsesExplicitNeeds(steps) {
		return uniqueNonEmpty(steps[i].Needs)
	}
	if i == 0 {
		return nil
	}
	prev := strings.TrimSpace(steps[i-1].ID)
	if prev == "" {
		return nil
	}
	return []string{prev}
}

// StepAncestorIDs returns the transitive predecessor set of steps[i] (not including itself).
func StepAncestorIDs(steps []WorkflowStep, i int) map[string]struct{} {
	out := make(map[string]struct{})
	if i < 0 || i >= len(steps) {
		return out
	}
	byID := stepIndexByID(steps)
	stack := append([]string(nil), StepNeedsIDs(steps, i)...)
	seen := make(map[string]struct{}, len(steps))
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out[id] = struct{}{}
		j, ok := byID[id]
		if !ok {
			continue
		}
		stack = append(stack, StepNeedsIDs(steps, j)...)
	}
	return out
}

func stepIndexByID(steps []WorkflowStep) map[string]int {
	m := make(map[string]int, len(steps))
	for i, st := range steps {
		id := strings.TrimSpace(st.ID)
		if id == "" {
			continue
		}
		if _, exists := m[id]; exists {
			continue
		}
		m[id] = i
	}
	return m
}

func uniqueNonEmpty(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func stepNeedsItemPos(st WorkflowStep, i int) Pos {
	if i >= 0 && i < len(st.NeedsPos) && !st.NeedsPos[i].IsZero() {
		return st.NeedsPos[i]
	}
	return st.Pos
}

// validateWorkflowGraph rejects dangling needs references and cycles (issue #192).
// Implicit sequential workflows (no needs declared) cannot cycle.
func validateWorkflowGraph(wfName string, w *WorkflowSpec) []error {
	if w == nil {
		return nil
	}
	steps := w.Steps
	byID := stepIndexByID(steps)
	var errs []error

	for _, st := range steps {
		sid := strings.TrimSpace(st.ID)
		seenNeed := make(map[string]int, len(st.Needs))
		for i, raw := range st.Needs {
			dep := strings.TrimSpace(raw)
			pos := stepNeedsItemPos(st, i)
			if dep == "" {
				errs = append(errs, pos.Errorf("workflow %s step %q: empty needs entry", wfName, sid))
				continue
			}
			if prev, dup := seenNeed[dep]; dup {
				errs = append(errs, pos.Errorf("workflow %s step %q: duplicate needs entry %q (already listed at index %d)", wfName, sid, dep, prev))
				continue
			}
			seenNeed[dep] = i
			if _, ok := byID[dep]; !ok {
				errs = append(errs, pos.Errorf("workflow %s step %q: needs references unknown step %q", wfName, sid, dep))
				continue
			}
			if dep == sid {
				errs = append(errs, pos.Errorf("workflow %s step %q: needs cycle involving %s", wfName, sid, sid))
			}
		}
	}

	n := len(steps)
	adj := make([][]int, n)
	for i := range steps {
		for _, dep := range StepNeedsIDs(steps, i) {
			j, ok := byID[dep]
			if !ok {
				continue
			}
			adj[i] = append(adj[i], j)
		}
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make([]int, n)
	var cycleFrom int
	var cycleTo int
	found := false
	var dfs func(int) bool
	dfs = func(u int) bool {
		color[u] = gray
		for _, v := range adj[u] {
			if color[v] == gray {
				cycleFrom, cycleTo = u, v
				return true
			}
			if color[v] == white && dfs(v) {
				return true
			}
		}
		color[u] = black
		return false
	}
	for i := range steps {
		if color[i] == white && dfs(i) {
			found = true
			break
		}
	}
	if found {
		st := steps[cycleFrom]
		sid := strings.TrimSpace(st.ID)
		target := strings.TrimSpace(steps[cycleTo].ID)
		pos := st.Pos
		for i, raw := range st.Needs {
			if strings.TrimSpace(raw) == target {
				pos = stepNeedsItemPos(st, i)
				break
			}
		}
		errs = append(errs, pos.Errorf("workflow %s step %q: needs cycle involving %s", wfName, sid, cyclePath(steps, adj, cycleFrom, cycleTo)))
	}
	return errs
}

func cyclePath(steps []WorkflowStep, adj [][]int, from, to int) string {
	ids := []string{strings.TrimSpace(steps[from].ID)}
	seen := map[int]struct{}{from: {}}
	cur := to
	for {
		ids = append(ids, strings.TrimSpace(steps[cur].ID))
		if cur == from {
			break
		}
		if _, ok := seen[cur]; ok {
			break
		}
		seen[cur] = struct{}{}
		next := -1
		for _, v := range adj[cur] {
			if _, ok := seen[v]; !ok || v == from {
				next = v
				if v == from {
					break
				}
			}
		}
		if next < 0 {
			break
		}
		cur = next
	}
	return strings.Join(ids, " -> ")
}
