package effects

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
)

type grant struct {
	tool, op, uses string
	unknown        bool
}

type reachedOp struct {
	tool, op, uses string
	unknown        bool
	witness        []Hop
}

type walker struct {
	g       *spec.ProjectGraph
	calls   map[string][]string
	stack   map[string]struct{}
	seen    map[string]struct{}
	reached []reachedOp
}

// Compute returns the transitive effect bound for every agent and workflow in g.
// g must already be resolved; Environment overlays are not applied.
func Compute(g *spec.ProjectGraph) GraphBounds {
	return compute(g, nil)
}

// compute walks g. workflowCalls is extra adjacency (from → to) used by tests to
// inject synthetic edges; production Compute passes nil and walks `workflow:` steps.
func compute(g *spec.ProjectGraph, workflowCalls map[string][]string) GraphBounds {
	out := GraphBounds{
		Agents:    map[string]Bound{},
		Workflows: map[string]Bound{},
	}
	if g == nil {
		return out
	}
	for _, name := range sortedKeys(g.Agents) {
		out.Agents[name] = boundAgent(g, name)
	}
	for _, name := range sortedKeys(g.Workflows) {
		out.Workflows[name] = boundWorkflow(g, name, workflowCalls)
	}
	return out
}

func boundAgent(g *spec.ProjectGraph, name string) Bound {
	w := newWalker(g, nil)
	prefix := []Hop{{Kind: KindAgent, Name: name, Reachability: Static}}
	w.walkAgent(name, prefix)
	return finish(g, KindAgent, name, w.reached)
}

func boundWorkflow(g *spec.ProjectGraph, name string, calls map[string][]string) Bound {
	w := newWalker(g, calls)
	prefix := []Hop{{Kind: KindWorkflow, Name: name, Reachability: Static}}
	w.walkWorkflow(name, prefix)
	return finish(g, KindWorkflow, name, w.reached)
}

func newWalker(g *spec.ProjectGraph, calls map[string][]string) *walker {
	return &walker{
		g:     g,
		calls: calls,
		stack: map[string]struct{}{},
		seen:  map[string]struct{}{},
	}
}

func (w *walker) walkWorkflow(name string, prefix []Hop) {
	key := "workflow:" + name
	if _, onStack := w.stack[key]; onStack {
		return
	}
	if _, done := w.seen[key]; done {
		return
	}
	w.stack[key] = struct{}{}
	defer func() {
		delete(w.stack, key)
		w.seen[key] = struct{}{}
	}()

	wf := w.g.Workflows[name]
	if wf == nil {
		return
	}
	for _, step := range wf.Spec.Steps {
		w.walkStep(step, prefix)
	}
	for _, other := range w.calls[name] {
		other = strings.TrimSpace(other)
		if other == "" {
			continue
		}
		w.walkWorkflow(other, appendHop(prefix, Hop{Kind: KindWorkflow, Name: other, Reachability: Static}))
	}
}

func (w *walker) walkStep(step spec.WorkflowStep, prefix []Hop) {
	id := strings.TrimSpace(step.ID)
	p := appendHop(prefix, Hop{Kind: KindStep, Name: id, ID: id, Reachability: Static})
	if uses := strings.TrimSpace(step.Uses); uses != "" {
		tn, op, err := tools.ParseUses(uses)
		if err != nil {
			w.addReached(p, grant{uses: uses, unknown: true}, Static)
		} else {
			w.addReached(p, grant{tool: tn, op: op, uses: uses}, Static)
		}
	}
	if ag := strings.TrimSpace(step.Agent); ag != "" {
		w.walkAgent(ag, appendHop(p, Hop{Kind: KindAgent, Name: ag, Reachability: Static}))
	}
	if callee := strings.TrimSpace(step.Workflow); callee != "" {
		w.walkWorkflow(callee, appendHop(p, Hop{Kind: KindWorkflow, Name: callee, Reachability: Static}))
	}
}

func (w *walker) walkAgent(name string, prefix []Hop) {
	key := "agent:" + name
	if _, onStack := w.stack[key]; onStack {
		return
	}
	if _, done := w.seen[key]; done {
		return
	}
	w.stack[key] = struct{}{}
	defer func() {
		delete(w.stack, key)
		w.seen[key] = struct{}{}
	}()

	agent := w.g.Agents[name]
	if agent == nil {
		w.addReached(prefix, grant{uses: name, unknown: true}, Autonomous)
		return
	}
	for _, g := range advertisedGrants(w.g, agent) {
		w.addReached(prefix, g, Autonomous)
	}
}

func (w *walker) addReached(prefix []Hop, g grant, reach Reachability) {
	name := g.uses
	if name == "" {
		name = g.tool
	}
	w.reached = append(w.reached, reachedOp{
		tool:    g.tool,
		op:      g.op,
		uses:    g.uses,
		unknown: g.unknown,
		witness: appendHop(prefix, Hop{Kind: KindToolOperation, Name: name, Reachability: reach}),
	})
}

func advertisedGrants(g *spec.ProjectGraph, agent *spec.AgentResource) []grant {
	listed, err := spec.ResolveAgentAdvertisedTools(agent, g.Tools)
	if err == nil {
		out := make([]grant, 0, len(listed))
		for _, a := range listed {
			out = append(out, parseGrant(a.Uses))
		}
		return out
	}
	// Fail-closed: do not treat a resolution error as "no grants / allow".
	var out []grant
	for _, raw := range agent.Spec.Tools {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		out = append(out, parseGrant(raw))
	}
	if len(out) == 0 {
		return []grant{{uses: agent.Metadata.Name, unknown: true}}
	}
	return out
}

func parseGrant(uses string) grant {
	tn, op, err := tools.ParseUses(uses)
	if err != nil {
		return grant{uses: uses, unknown: true}
	}
	return grant{tool: tn, op: op, uses: uses}
}

func finish(g *spec.ProjectGraph, rootKind HopKind, rootName string, reached []reachedOp) Bound {
	first := map[string]reachedOp{}
	order := make([]string, 0, len(reached))
	for _, r := range reached {
		key := r.uses
		if key == "" {
			key = r.tool + "\x00" + r.op
		}
		if prev, ok := first[key]; ok {
			first[key] = preferAutonomousReached(prev, r)
			continue
		}
		first[key] = r
		order = append(order, key)
	}

	var effects []Effect
	seenIdent := map[string]int{}
	for _, key := range order {
		r := first[key]
		if r.unknown {
			effects = append(effects, Effect{
				Unknown: true,
				Message: unknownMessage(r.tool, r.op, r.uses, nil),
				Uses:    r.uses,
				Witness: r.witness,
			})
			continue
		}
		var tsp *spec.ToolSpec
		if tr := g.Tools[r.tool]; tr != nil {
			tsp = &tr.Spec
		}
		re := spec.ResolveOperationEffects(r.tool, r.op, tsp)
		if re.Unknown {
			effects = append(effects, Effect{
				Unknown: true,
				Message: unknownMessage(r.tool, r.op, r.uses, tsp),
				Uses:    r.uses,
				Witness: r.witness,
			})
			continue
		}
		for _, ident := range re.Effects {
			if i, ok := seenIdent[ident]; ok {
				effects[i].occurrences = appendOccurrence(effects[i].occurrences, r.uses, r.witness)
				preferAutonomousEffect(&effects[i], r)
				continue
			}
			seenIdent[ident] = len(effects)
			effects = append(effects, Effect{
				Ident:   ident,
				Uses:    r.uses,
				Witness: r.witness,
				occurrences: []effectOccurrence{{
					uses:    r.uses,
					witness: r.witness,
				}},
			})
		}
	}
	sort.Slice(effects, func(i, j int) bool {
		a, b := effects[i], effects[j]
		if a.Unknown != b.Unknown {
			return a.Unknown
		}
		if a.Unknown {
			return a.Uses < b.Uses
		}
		return a.Ident < b.Ident
	})

	return Bound{
		RootKind:    rootKind,
		RootName:    rootName,
		Effects:     effects,
		Unreachable: unreachableFrom(g, first),
	}
}

func unreachableFrom(g *spec.ProjectGraph, reached map[string]reachedOp) []Unreachable {
	var out []Unreachable
	for _, toolName := range sortedKeys(g.Tools) {
		tr := g.Tools[toolName]
		if tr == nil {
			continue
		}
		te := spec.ResolveToolEffects(toolName, &tr.Spec)
		if te.Unknown {
			continue
		}
		for _, op := range sortedKeys(te.ByOperation) {
			uses := "tool." + toolName + "." + op
			if _, ok := reached[uses]; ok {
				continue
			}
			idents := te.ByOperation[op]
			if len(idents) == 0 {
				out = append(out, Unreachable{
					Unknown:   true,
					Tool:      toolName,
					Operation: op,
					Uses:      uses,
				})
				continue
			}
			for _, ident := range idents {
				out = append(out, Unreachable{
					Ident:     ident,
					Tool:      toolName,
					Operation: op,
					Uses:      uses,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Uses != out[j].Uses {
			return out[i].Uses < out[j].Uses
		}
		return out[i].Ident < out[j].Ident
	})
	return out
}

func unknownMessage(tool, op, uses string, tsp *spec.ToolSpec) string {
	name := strings.TrimSpace(tool)
	if name == "" {
		name = strings.TrimSpace(uses)
	}
	if name == "" {
		name = "(unnamed)"
	}
	te := spec.ResolveToolEffects(name, tsp)
	if te.Unknown && te.Message != "" {
		return te.Message
	}
	if strings.TrimSpace(op) == "" {
		return fmt.Sprintf("Tool/%s: no declared effects (fail-closed unknown; no policy permits this tool unless it opts in)", name)
	}
	return fmt.Sprintf("Tool/%s: operation %q has no declared effects (fail-closed unknown; no policy permits this operation unless it opts in)", name, op)
}

// preferAutonomousReached keeps the autonomous witnessing path when the same
// operation is reached both as a static uses: and as an agent grant (path-max).
func preferAutonomousReached(prev, next reachedOp) reachedOp {
	if pathReachability(next.witness) == Autonomous && pathReachability(prev.witness) != Autonomous {
		return next
	}
	return prev
}

func preferAutonomousEffect(e *Effect, r reachedOp) {
	if e == nil {
		return
	}
	if pathReachability(r.witness) == Autonomous && pathReachability(e.Witness) != Autonomous {
		e.Witness = r.witness
		e.Uses = r.uses
	}
}

func pathReachability(hops []Hop) Reachability {
	for _, h := range hops {
		if h.Reachability == Autonomous {
			return Autonomous
		}
	}
	return Static
}

func appendOccurrence(dst []effectOccurrence, uses string, witness []Hop) []effectOccurrence {
	for _, o := range dst {
		if o.uses == uses {
			return dst
		}
	}
	return append(dst, effectOccurrence{uses: uses, witness: witness})
}

func appendHop(prefix []Hop, h Hop) []Hop {
	out := make([]Hop, len(prefix)+1)
	copy(out, prefix)
	out[len(prefix)] = h
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
