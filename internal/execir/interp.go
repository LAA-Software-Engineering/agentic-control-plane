package execir

import (
	"context"
	"fmt"
	"sync"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// DefaultMaxConcurrency bounds goroutine fan-out for a parallel Loop or Fork
// when the caller sets no override. It mirrors the engine's step-concurrency
// default without importing the engine (execir must stay runtime-independent).
const DefaultMaxConcurrency = 8

// Invoker performs the effectful leaf operations. The execution IR carries no
// I/O of its own; an engine adapter supplies one that runs a real tool/agent/
// subworkflow, while tests supply a recording stub. Implementations must be safe
// for concurrent use — a parallel Loop or Fork invokes from several goroutines.
type Invoker interface {
	InvokeTool(ctx context.Context, uses string, args map[string]any) (any, error)
	InvokeAgent(ctx context.Context, agent string, args map[string]any) (any, error)
	InvokeWorkflow(ctx context.Context, workflow string, args map[string]any) (any, error)
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
	if err := r.execAll(scope, prog.Body); err != nil {
		return nil, err
	}
	return r.output, nil
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
func (r *runner) execAll(scope map[string]any, nodes []Node) error {
	for _, n := range nodes {
		if r.done {
			return nil
		}
		if err := r.exec(scope, n); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) exec(scope map[string]any, n Node) error {
	switch v := n.(type) {
	case *InvokeTool:
		return r.invoke(scope, v.Bind, v.Args, func(a map[string]any) (any, error) {
			return r.in.Invoker.InvokeTool(r.ctx, v.Uses, a)
		})
	case *InvokeAgent:
		return r.invoke(scope, v.Bind, v.Args, func(a map[string]any) (any, error) {
			return r.in.Invoker.InvokeAgent(r.ctx, v.Agent, a)
		})
	case *InvokeWorkflow:
		return r.invoke(scope, v.Bind, v.Args, func(a map[string]any) (any, error) {
			return r.in.Invoker.InvokeWorkflow(r.ctx, v.Workflow, a)
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
		return r.execBranch(scope, v)
	case *Fork:
		return r.execFork(scope, v)
	case *Loop:
		return r.execLoop(scope, v)
	case *Return:
		val, err := evalValue(scope, v.Value)
		if err != nil {
			return err
		}
		r.output = val
		r.done = true
		return nil
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

func (r *runner) execBranch(scope map[string]any, b *Branch) error {
	cond, err := evalExpr(scope, b.Cond)
	if err != nil {
		return err
	}
	if cond {
		return r.execAll(scope, b.Then)
	}
	return r.execAll(scope, b.Else)
}

// execFork runs statically-known branches concurrently and joins before
// returning. Each branch executes in its own child scope so branches cannot race
// on shared writes; after the join, each branch's published binding (ForkBranch.
// Bind) is copied into the parent scope. A Return inside a Fork branch is not
// permitted to escape the join — the surface never lowers one there — so branch
// runners do not propagate r.done.
func (r *runner) execFork(scope map[string]any, f *Fork) error {
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
			if err := sub.execAll(child, br.Nodes); err != nil {
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

func (r *runner) execLoop(scope map[string]any, l *Loop) error {
	items, err := evalCollection(scope, l.Collection)
	if err != nil {
		return err
	}
	if max := r.in.maxIters(); len(items) > max {
		return fmt.Errorf("execir: loop over %d items exceeds the maximum of %d iterations (raise limits.maxLoopIterations)", len(items), max)
	}
	if l.Parallel {
		return r.execLoopParallel(scope, l, items)
	}
	for _, item := range items {
		child := childScope(scope)
		child[l.Var] = item
		sub := &runner{in: r.in, ctx: r.ctx}
		if err := sub.execAll(child, l.Body); err != nil {
			return err
		}
	}
	return nil
}

// execLoopParallel is dynamic fan-out: one iteration per collection element,
// running with bounded concurrency. Each iteration has an isolated child scope,
// so iterations never race, and a body binding does not escape the loop.
func (r *runner) execLoopParallel(scope map[string]any, l *Loop, items []any) error {
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
			errs[i] = sub.execAll(child, l.Body)
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

// childScope shallow-copies a scope so a nested block (loop iteration, fork
// branch) can bind names without mutating or racing the parent.
func childScope(parent map[string]any) map[string]any {
	child := make(map[string]any, len(parent)+1)
	for k, v := range parent {
		child[k] = v
	}
	return child
}
