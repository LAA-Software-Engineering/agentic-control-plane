package execir

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

// DefaultMaxConcurrency bounds goroutine fan-out for a parallel Loop or Fork
// when the caller sets no override. It mirrors the engine's step-concurrency
// default without importing the engine (execir must stay runtime-independent).
const DefaultMaxConcurrency = 8

// CallSite is the structural identity of one leaf invocation, passed to the
// [Invoker] per call (issue #257). It is the key an engine adapter uses for
// trace/persistence identity and the key Phase 2 durability (#258) memoizes a
// completed leaf under — which is why the ABI carries it now, before the
// interface freezes.
//
// It is a per-call VALUE, built fresh as the interpreter walks and never shared
// mutably, so it is goroutine-safe by construction: a parallel Loop, Fork, or
// Graph invokes from several goroutines, each carrying its own CallSite.
//
//   - Bind is the binding name the result is published under — the step id on the
//     YAML path, an assignment target or generated temp on the `.agent` path. It
//     may be empty for an effect-only call, so it is not a sufficient key alone.
//   - Path is the static node address: the child index at each nesting level from
//     the program root, so every static node position is distinct even when Bind
//     is empty or repeats. For a [Graph] node the segment is the node's CANONICAL
//     (id-sorted) rank, not its authored index, so Path is stable under the
//     digest-preserving step reorderings #256 treats as equal — the identity
//     #258 memoizes on must not shift under a semantically-neutral edit.
//   - Loop is the enclosing loop iteration indices (outermost first), so the same
//     static node executed on different iterations has distinct identity. It is
//     empty on the YAML path (no loops) and non-empty only under `.agent` loops.
type CallSite struct {
	Bind string
	Path []int
	Loop []int
}

// Invoker performs the effectful leaf operations. The execution IR carries no
// I/O of its own; an engine adapter supplies one that runs a real tool/agent/
// subworkflow, while tests supply a recording stub. Implementations must be safe
// for concurrent use — a parallel Loop, Fork, or Graph invokes from several
// goroutines. The [CallSite] identifies the invocation (issue #257).
type Invoker interface {
	InvokeTool(ctx context.Context, site CallSite, uses string, args map[string]any) (any, error)
	InvokeAgent(ctx context.Context, site CallSite, agent string, args map[string]any) (any, error)
	InvokeWorkflow(ctx context.Context, site CallSite, workflow string, args map[string]any) (any, error)
}

// Interp executes a Program against an Invoker. Zero values pick built-in
// defaults: MaxLoopIterations falls back to spec.DefaultMaxLoopIterations and
// MaxConcurrency to DefaultMaxConcurrency.
type Interp struct {
	Invoker           Invoker
	MaxLoopIterations int
	MaxConcurrency    int
}

func (in *Interp) maxIters() int {
	if in.MaxLoopIterations > 0 {
		return in.MaxLoopIterations
	}
	return spec.DefaultMaxLoopIterations
}

func (in *Interp) maxConc() int {
	if in.MaxConcurrency > 0 {
		return in.MaxConcurrency
	}
	return DefaultMaxConcurrency
}

// Run executes prog with the given workflow input and returns the value set by a
// Return node (nil if the program returns nothing).
func (in *Interp) Run(ctx context.Context, prog *Program, input map[string]any) (any, error) {
	if in == nil || in.Invoker == nil {
		return nil, fmt.Errorf("execir: nil interpreter or invoker")
	}
	if prog == nil {
		return nil, fmt.Errorf("execir: nil program")
	}
	scope := paramScope(prog.Params, input)
	r := &runner{in: in, ctx: ctx}
	if err := r.execAll(scope, prog.Body, nil, nil); err != nil {
		return nil, err
	}
	return r.output, nil
}

// extend returns a fresh slice base+[x], never aliasing base — a child address
// must not mutate its parent's (several goroutines share the parent).
func extend(base []int, x int) []int {
	out := make([]int, len(base)+1)
	copy(out, base)
	out[len(base)] = x
	return out
}

// paramScope binds workflow parameters into the initial scope, matching the
// resource lowering's convention (internal/lang/lower newEnv): a single
// parameter names the whole workflow input, so `input.repo` (or `pr.repo` for a
// parameter named `pr`) resolves against the entire input document; multiple
// parameters each name one top-level field of the input.
func paramScope(params []string, input map[string]any) map[string]any {
	scope := make(map[string]any, len(params)+1)
	switch {
	case len(params) == 1:
		scope[params[0]] = input
	default:
		for _, p := range params {
			if input != nil {
				scope[p] = input[p]
			}
		}
	}
	return scope
}

type runner struct {
	in     *Interp
	ctx    context.Context
	output any
	done   bool // a Return has fired; stop executing subsequent nodes
}

// execAll runs nodes in order against scope, stopping early once a Return fires.
// path is the address of the enclosing node list; child i extends it. loop is the
// enclosing loop-iteration indices.
func (r *runner) execAll(scope map[string]any, nodes []Node, path, loop []int) error {
	for i, n := range nodes {
		if r.done {
			return nil
		}
		if err := r.exec(scope, n, extend(path, i), loop); err != nil {
			return err
		}
	}
	return nil
}

// exec runs one node. path is this node's own address; loop is the enclosing
// loop-iteration indices. Both are folded into the [CallSite] of any leaf
// invocation the node performs.
func (r *runner) exec(scope map[string]any, n Node, path, loop []int) error {
	switch v := n.(type) {
	case *InvokeTool:
		site := CallSite{Bind: v.Bind, Path: path, Loop: loop}
		return r.invoke(scope, v.Bind, v.Args, func(a map[string]any) (any, error) {
			return r.in.Invoker.InvokeTool(r.ctx, site, v.Uses, a)
		})
	case *InvokeAgent:
		site := CallSite{Bind: v.Bind, Path: path, Loop: loop}
		return r.invoke(scope, v.Bind, v.Args, func(a map[string]any) (any, error) {
			return r.in.Invoker.InvokeAgent(r.ctx, site, v.Agent, a)
		})
	case *InvokeWorkflow:
		site := CallSite{Bind: v.Bind, Path: path, Loop: loop}
		return r.invoke(scope, v.Bind, v.Args, func(a map[string]any) (any, error) {
			return r.in.Invoker.InvokeWorkflow(r.ctx, site, v.Workflow, a)
		})
	case *Let:
		val, err := evalValue(scope, v.Value)
		if err != nil {
			return err
		}
		if v.Bind != "" {
			scope[v.Bind] = val
		}
		return nil
	case *Branch:
		return r.execBranch(scope, v, path, loop)
	case *Fork:
		return r.execFork(scope, v, path, loop)
	case *Loop:
		return r.execLoop(scope, v, path, loop)
	case *Graph:
		return r.execGraph(scope, v, path, loop)
	case *Return:
		val, err := evalValue(scope, v.Value)
		if err != nil {
			return err
		}
		r.output = val
		r.done = true
		return nil
	case *Approval:
		// A human pause suspends and resumes through the engine's checkpoint
		// machinery (Phase 2, #258); treating it as a no-op here would silently
		// skip a gate, so it fails loudly.
		return fmt.Errorf("execir: Approval node execution is not implemented (durable suspend/resume, issue #258)")
	default:
		return fmt.Errorf("execir: unknown node %T", n)
	}
}

func (r *runner) invoke(scope map[string]any, bind string, args map[string]Value, call func(map[string]any) (any, error)) error {
	a, err := evalArgs(scope, args)
	if err != nil {
		return err
	}
	res, err := call(a)
	if err != nil {
		return err
	}
	if bind != "" {
		scope[bind] = res
	}
	return nil
}

func (r *runner) execBranch(scope map[string]any, b *Branch, path, loop []int) error {
	cond, err := evalExpr(scope, b.Cond)
	if err != nil {
		return err
	}
	if cond {
		return r.execAll(scope, b.Then, extend(path, 0), loop)
	}
	return r.execAll(scope, b.Else, extend(path, 1), loop)
}

// execFork runs statically-known branches concurrently and joins before
// returning. Each branch executes in its own child scope so branches cannot race
// on shared writes; after the join, each branch's published binding (ForkBranch.
// Bind) is copied into the parent scope. A Return inside a Fork branch is not
// permitted to escape the join — the surface never lowers one there — so branch
// runners do not propagate r.done.
func (r *runner) execFork(scope map[string]any, f *Fork, path, loop []int) error {
	type result struct {
		bind string
		val  any
		ok   bool
		err  error
	}
	results := make([]result, len(f.Branches))
	sem := make(chan struct{}, r.in.maxConc())
	var wg sync.WaitGroup
	for i, br := range f.Branches {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, br ForkBranch) {
			defer wg.Done()
			defer func() { <-sem }()
			child := childScope(scope)
			sub := &runner{in: r.in, ctx: r.ctx}
			if err := sub.execAll(child, br.Nodes, extend(path, i), loop); err != nil {
				results[i] = result{err: err}
				return
			}
			val, ok := child[br.Bind]
			results[i] = result{bind: br.Bind, val: val, ok: ok}
		}(i, br)
	}
	wg.Wait()
	for _, res := range results {
		if res.err != nil {
			return res.err
		}
		if res.bind != "" && res.ok {
			scope[res.bind] = res.val
		}
	}
	return nil
}

func (r *runner) execLoop(scope map[string]any, l *Loop, path, loop []int) error {
	items, err := evalCollection(scope, l.Collection)
	if err != nil {
		return err
	}
	if max := r.in.maxIters(); len(items) > max {
		return fmt.Errorf("execir: loop over %d items exceeds the maximum of %d iterations (raise limits.maxLoopIterations)", len(items), max)
	}
	if l.Parallel {
		return r.execLoopParallel(scope, l, items, path, loop)
	}
	// A sequential loop shares the enclosing scope and runner: the loop variable
	// and any body binding write to it (last iteration wins and escapes, like the
	// straight-line body the type checker walks), and a Return inside the body
	// fires on this runner — it returns from the workflow and halts the loop,
	// rather than being swallowed as a per-iteration no-op. This is the SAME
	// scope/Return rule the checker uses for sequential control flow; the parallel
	// path below is the sole exception, and `return` is not lowered into it.
	for idx, item := range items {
		scope[l.Var] = item
		if err := r.execAll(scope, l.Body, path, extend(loop, idx)); err != nil {
			return err
		}
		if r.done {
			break
		}
	}
	return nil
}

// execLoopParallel is dynamic fan-out: one iteration per collection element,
// running with bounded concurrency. Each iteration has an isolated child scope,
// so iterations never race, and a body binding does not escape the loop.
func (r *runner) execLoopParallel(scope map[string]any, l *Loop, items []any, path, loop []int) error {
	errs := make([]error, len(items))
	sem := make(chan struct{}, r.in.maxConc())
	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, item any) {
			defer wg.Done()
			defer func() { <-sem }()
			child := childScope(scope)
			child[l.Var] = item
			sub := &runner{in: r.in, ctx: r.ctx}
			errs[i] = sub.execAll(child, l.Body, path, extend(loop, i))
		}(i, item)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// execGraph runs a general needs-DAG (issue #256/#257): each node runs as soon
// as ITS predecessors complete, with bounded concurrency, publishing its result
// into scope under its id. This is the join-accurate counterpart to Fork's
// all-branches barrier — in `A,B roots; C[A]; D[A,B]; E[C]`, D runs when A and B
// finish, not when C/E do — mirroring the engine DAG's depsReady scheduling
// (internal/engine/execution_dag.go) minus checkpoint/HITL. A Return does not
// occur inside a Graph node (its nodes are Invoke*/Approval), so r.done is not
// propagated from here.
func (r *runner) execGraph(scope map[string]any, g *Graph, path, loop []int) error {
	n := len(g.Nodes)
	if n == 0 {
		return nil
	}
	var mu sync.Mutex // guards scope writes, completed, running, firstErr
	completed := make(map[string]struct{}, n)
	running := make(map[int]struct{}, n)
	var firstErr error
	sem := make(chan struct{}, r.in.maxConc())
	completion := make(chan struct{}, n)
	var wg sync.WaitGroup
	// On the first failure, cancel in-flight siblings so a partial-failure graph
	// stops promptly — matching the engine DAG, which cancels the run context on a
	// step error rather than letting siblings run to completion.
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()

	// rank maps a node's authored index to its CANONICAL (id-sorted) rank, used as
	// the node's CallSite.Path segment. Authored order in a Graph is semantically
	// neutral — reordering independent steps is a digest-preserving edit
	// (#256 encodeGraph sorts by id) — so keying the address on authored index
	// would make the frozen identity disagree with the digest and shift under a
	// no-op reorder. Ranking by id (tie-broken by index for determinism if two
	// ids ever coincide) makes Path stable under exactly the reorderings the
	// digest calls equal, which is the property #258 memoization needs.
	rank := canonicalRanks(g.Nodes)

	ready := func(gn GraphNode) bool {
		for _, dep := range gn.Needs {
			if _, ok := completed[dep]; !ok {
				return false
			}
		}
		return true
	}

	// trySchedule launches every currently-runnable node. Caller holds mu.
	trySchedule := func() {
		if firstErr != nil {
			return
		}
		for i, gn := range g.Nodes {
			if _, done := completed[gn.ID]; done {
				continue
			}
			if _, run := running[i]; run {
				continue
			}
			if !ready(gn) {
				continue
			}
			running[i] = struct{}{}
			wg.Add(1)
			go func(i int, gn GraphNode) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				// Snapshot the scope so arg evaluation (which reads predecessor
				// outputs, all already published under mu) never races a
				// concurrent sibling's publish.
				mu.Lock()
				local := childScope(scope)
				mu.Unlock()
				sub := &runner{in: r.in, ctx: ctx}
				err := sub.exec(local, gn.Run, extend(path, rank[i]), loop)
				mu.Lock()
				if err != nil {
					if firstErr == nil {
						firstErr = err
						cancel()
					}
				} else {
					if gn.ID != "" {
						if v, ok := local[gn.ID]; ok {
							scope[gn.ID] = v
						}
					}
					completed[gn.ID] = struct{}{}
				}
				delete(running, i)
				mu.Unlock()
				completion <- struct{}{}
			}(i, gn)
		}
	}

	mu.Lock()
	trySchedule()
	for {
		allDone := len(completed) == n
		stuck := firstErr == nil && !allDone && len(running) == 0
		if firstErr != nil || allDone || stuck {
			mu.Unlock()
			break
		}
		mu.Unlock()
		<-completion
		mu.Lock()
		trySchedule()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if firstErr != nil {
		return firstErr
	}
	if len(completed) != n {
		return fmt.Errorf("execir: graph has no runnable step (unsatisfiable needs or cycle)")
	}
	return nil
}

// canonicalRanks returns, for each node's authored index, its position when the
// nodes are ordered by id (ties broken by authored index). Two Graphs that
// denote the same DAG with independent steps reordered therefore assign every id
// the same rank — so a node's CallSite.Path is invariant under the
// digest-preserving reorderings #256 canonicalizes away.
func canonicalRanks(nodes []GraphNode) []int {
	order := make([]int, len(nodes))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool {
		if nodes[order[a]].ID != nodes[order[b]].ID {
			return nodes[order[a]].ID < nodes[order[b]].ID
		}
		return order[a] < order[b]
	})
	rank := make([]int, len(nodes))
	for pos, idx := range order {
		rank[idx] = pos
	}
	return rank
}

// childScope shallow-copies a scope so a nested block (loop iteration, fork
// branch) can bind names without mutating or racing the parent.
func childScope(parent map[string]any) map[string]any {
	child := make(map[string]any, len(parent)+1)
	for k, v := range parent {
		child[k] = v
	}
	return child
}
