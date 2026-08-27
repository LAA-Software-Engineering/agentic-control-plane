package lower

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/LAA-Software-Engineering/terfyn/internal/lang"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

// Options carries classification the current file cannot supply on its own.
type Options struct {
	// Workflows names EXTRA callees — project-level workflows declared in other
	// files — that a single-identifier call should lower to a workflow: step.
	// Workflows declared in the file being lowered are detected automatically and
	// need not be listed here; a name declared in the file always wins over this
	// set. Dotted callees (github.get_pr) are always tool calls. Once #198 lands
	// its project-wide symbol table replaces this field.
	Workflows map[string]bool
}

// Result is the resource projection of a lowered .agent file (ADR 002 §5): the
// Agent/Tool/Policy/Workflow resources plan/apply/policy analysis run against. It
// is deliberately NOT an input to the execution lowering (#199) — see doc.go.
//
// A Result is a valid resource projection only when LowerFile returned NO
// diagnostics. When diagnostics are present the Result is best-effort: it may
// carry duplicate resource names and invalid interpolation tokens (e.g. a
// whole-input ${input} that engine.resolvePath rejects), because lowering does
// not drop a written construct without a diagnostic. Callers must check the
// diagnostics before using a Result. In particular LowerFile is the authority for
// resource identity in a file — it reports a duplicate agent/workflow name, or a
// name declared as both — so ToGraph and project.MergeLowered may assume unique
// names on a diagnostic-free Result.
type Result struct {
	Agents    []*spec.AgentResource
	Workflows []*spec.WorkflowResource
	SourceMap *SourceMap
}

// ToGraph folds the lowered resources into a fresh spec.ProjectGraph keyed by
// name. It does not merge with any existing graph; use project.MergeLowered for
// that. It assumes a diagnostic-free Result (unique names, per the Result doc);
// duplicate names would collapse under the map, which is why LowerFile diagnoses
// them.
func (r *Result) ToGraph() *spec.ProjectGraph {
	g := &spec.ProjectGraph{
		Agents:       map[string]*spec.AgentResource{},
		Tools:        map[string]*spec.ToolResource{},
		Workflows:    map[string]*spec.WorkflowResource{},
		Policies:     map[string]*spec.PolicyResource{},
		Environments: map[string]*spec.EnvironmentResource{},
	}
	if r == nil {
		return g
	}
	for _, a := range r.Agents {
		g.Agents[a.Metadata.Name] = a
	}
	for _, w := range r.Workflows {
		g.Workflows[w.Metadata.Name] = w
	}
	return g
}

// LowerFile lowers a parsed .agent file into its resource projection. Diagnostics
// are lowering-time problems (an unresolved reference, a malformed call); an
// empty result is returned for a nil file.
func LowerFile(f *lang.File, opts Options) (*Result, lang.Diagnostics) {
	l := &lowerer{opts: opts, sm: newSourceMap()}
	res := &Result{SourceMap: l.sm}
	if f == nil {
		return res, nil
	}
	// A pre-pass over the whole file establishes resource identity before any
	// body is lowered: which single-identifier callees are workflows vs agents
	// (a call may name a workflow declared later in the file), and which names
	// collide. The classifier this builds is the answer the File already holds —
	// callee kind is not deferred to #198's project-wide resolution.
	l.classifyDecls(f)
	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *lang.AgentDecl:
			if a := l.agent(decl); a != nil {
				res.Agents = append(res.Agents, a)
			}
		case *lang.WorkflowDecl:
			if w := l.workflow(decl); w != nil {
				res.Workflows = append(res.Workflows, w)
			}
		}
	}
	return res, l.diags
}

type lowerer struct {
	opts  Options
	sm    *SourceMap
	diags lang.Diagnostics
	// workflowCallee[name] is true when a single-identifier call to name lowers
	// to a workflow: step. Built by classifyDecls from the file's WorkflowDecls
	// (and Options.Workflows); an agent-declared name is never marked.
	workflowCallee map[string]bool
}

// classifyDecls records, for every top-level declaration, whether its name is an
// agent or a workflow, so a single-identifier callee lowers to the right step
// kind. A name declared twice (same kind) or as both an agent and a workflow is a
// diagnostic — the file is the first place resource identity exists, so the
// ambiguity is reported here, never resolved by silently choosing agent:.
func (l *lowerer) classifyDecls(f *lang.File) {
	l.workflowCallee = map[string]bool{}
	kind := map[string]string{}
	at := map[string]spec.Pos{}
	for _, d := range f.Decls {
		var name, k string
		var pos spec.Pos
		switch decl := d.(type) {
		case *lang.AgentDecl:
			name, k, pos = identName(decl.Name), "agent", declNamePos(decl.Name, decl.Pos)
		case *lang.WorkflowDecl:
			name, k, pos = identName(decl.Name), "workflow", declNamePos(decl.Name, decl.Pos)
		default:
			continue
		}
		if name == "" {
			continue
		}
		if prev, ok := kind[name]; ok {
			if prev == k {
				l.diag(pos, "duplicate %s %q (already declared at %s)", k, name, at[name].String())
			} else {
				l.diag(pos, "%q is declared as both an agent and a workflow", name)
			}
			continue // first declaration wins the classification
		}
		kind[name] = k
		at[name] = pos
		if k == "workflow" {
			l.workflowCallee[name] = true
		}
	}
	// Extra project-level workflow names from other files, but never overriding a
	// name this file declares as an agent.
	for name := range l.opts.Workflows {
		if _, declared := kind[name]; !declared {
			l.workflowCallee[name] = true
		}
	}
}

func declNamePos(id *lang.Ident, fallback spec.Pos) spec.Pos {
	if id != nil && !id.Pos.IsZero() {
		return id.Pos
	}
	return fallback
}

func (l *lowerer) diag(p spec.Pos, format string, args ...any) {
	l.diags = append(l.diags, lang.Diagnostic{Pos: p, Msg: fmt.Sprintf(format, args...)})
}

// --- Agents -----------------------------------------------------------------

func (l *lowerer) agent(d *lang.AgentDecl) *spec.AgentResource {
	name := identName(d.Name)
	if name == "" {
		l.diag(d.Pos, "agent declaration has no name")
		return nil
	}
	ar := &spec.AgentResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindAgent,
		Metadata:   spec.Metadata{Name: name},
		Pos:        d.Pos,
	}
	l.sm.set(KeyAgent(name), d.Pos)
	if d.Model != nil {
		ar.Spec.Model = d.Model.Raw
	}
	if d.Policy != nil {
		ar.Spec.Policy = d.Policy.Name
	}
	// grants -> AgentSpec.Tools: an autonomous capability bound, not a call list
	// (ADR 002). Each grant reconstructs the tool.<name>.<operation> uses string.
	//
	// NOTE (Epic F gap): grants may bind several operations on ONE tool
	// (tool.github.read_pr + tool.github.read_comments). AgentSpec.Tools as
	// consumed by the #160 agent loop (ResolveAgentAdvertisedTools) currently
	// permits only one operation per tool, so the ADR 002 fixture does not yet
	// pass full agent-spec validation. Lowering preserves every grant faithfully;
	// lifting the one-operation-per-tool limit is #188/#204, not #197. See
	// TestLower_MultiOperationGrantIsKnownGap.
	for _, g := range d.Grants {
		uses := grantUses(g)
		if uses == "" {
			// A malformed grant is already a parser diagnostic; skip silently so
			// lowering does not double-report.
			continue
		}
		ar.Spec.Tools = append(ar.Spec.Tools, uses)
		ar.Spec.ToolsPos = append(ar.Spec.ToolsPos, g.Pos)
		l.sm.set(KeyAgentGrant(name, uses), g.Pos)
	}
	// Type references are recorded in the source map, not lowered into schema:
	// fields — those name compiled schema files by path, and a bare .agent type
	// name would fail schema-file validation. Resolution is #193/#198.
	if d.Input != nil {
		l.sm.set(KeyAgentType(name, "input"), d.Input.Pos)
	}
	if d.Output != nil {
		l.sm.set(KeyAgentType(name, "output"), d.Output.Pos)
	}
	return ar
}

func grantUses(g *lang.Grant) string {
	tn := g.ToolName()
	op := g.OperationName()
	if tn == "" || op == "" {
		return ""
	}
	return "tool." + tn + "." + op
}

// --- Workflows --------------------------------------------------------------

func (l *lowerer) workflow(d *lang.WorkflowDecl) *spec.WorkflowResource {
	name := identName(d.Name)
	if name == "" {
		l.diag(d.Pos, "workflow declaration has no name")
		return nil
	}
	wr := &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindWorkflow,
		Metadata:   spec.Metadata{Name: name},
		Pos:        d.Pos,
	}
	l.sm.set(KeyWorkflow(name), d.Pos)

	// Param/result types and the effects { } clause are diagnostic-only for the
	// resource projection (checking is #198/#190); record positions, do not
	// synthesize resource fields.
	for _, p := range d.Params {
		if p.Name != nil {
			l.sm.set(KeyWorkflowType(name, p.Name.Name), p.Pos)
		}
	}
	if d.Result != nil {
		l.sm.set(KeyWorkflowType(name, "result"), d.Result.Pos)
	}
	for _, e := range d.Effects {
		l.sm.set(KeyWorkflowEffect(name, e.Name), e.Pos)
	}

	wl := &workflowLowerer{
		l:    l,
		wf:   name,
		env:  newEnv(d),
		used: map[string]int{},
	}
	wl.reserveBindings(d.Body)
	wl.lowerBody(d.Body)
	wr.Spec.Steps = wl.steps
	wr.Spec.Output = wl.output
	return wr
}

// refEnv maps a leading source identifier to its interpolation prefix (without
// the surrounding ${}). Params resolve to the input root; bindings resolve to
// their step output.
type refEnv struct {
	roots map[string]string
}

func newEnv(d *lang.WorkflowDecl) refEnv {
	roots := map[string]string{}
	switch {
	case len(d.Params) == 1 && d.Params[0].Name != nil:
		// A single parameter names the whole workflow input, so a field access
		// (input.repo) lowers to ${input.repo}. A BARE reference to that
		// parameter would lower to ${input}, which the interpolation language
		// cannot represent — token() diagnoses that case.
		roots[d.Params[0].Name.Name] = "input"
	default:
		for _, p := range d.Params {
			if p.Name != nil {
				roots[p.Name.Name] = "input." + p.Name.Name
			}
		}
	}
	return refEnv{roots: roots}
}

type workflowLowerer struct {
	l      *lowerer
	wf     string
	env    refEnv
	used   map[string]int // reserved + generated step ids
	steps  []spec.WorkflowStep
	output *spec.WorkflowOutput
}

// reserveBindings claims every explicit binding name so generated (unbound / SSA
// temporary) step ids never collide with an author's binding.
func (wl *workflowLowerer) reserveBindings(body []lang.Stmt) {
	for _, st := range body {
		switch s := st.(type) {
		case *lang.AssignStmt:
			wl.reserveName(s.Target)
		case *lang.ParallelStmt:
			for _, b := range s.Body {
				wl.reserveName(b.Target)
			}
		}
	}
}

func (wl *workflowLowerer) reserveName(id *lang.Ident) {
	if id == nil || id.Name == "" {
		return
	}
	if _, ok := wl.used[id.Name]; ok {
		wl.l.diag(id.Pos, "duplicate binding %q", id.Name)
		return
	}
	wl.used[id.Name] = 0
}

// lowerBody walks top-level statements, threading a dependency frontier: the
// step ids the next statement must wait for. Sequential statements chain to the
// previous frontier (order-preserving); a parallel block's branches all share
// the pre-block frontier and their union becomes the next frontier (fan-in).
func (wl *workflowLowerer) lowerBody(body []lang.Stmt) {
	var frontier []string
	for _, st := range body {
		switch s := st.(type) {
		case *lang.AssignStmt:
			if id, ok := wl.lowerAssign(s, frontier); ok {
				frontier = []string{id}
			}
		case *lang.ExprStmt:
			call, ok := s.X.(*lang.CallExpr)
			if !ok {
				wl.l.diag(s.Pos, "expression statement is not a call and has no effect")
				continue
			}
			id := wl.freshID(calleeLeaf(call.Callee))
			wl.lowerCall(id, call, frontier, s.Pos)
			frontier = []string{id}
		case *lang.ParallelStmt:
			var next []string
			for _, b := range s.Body {
				if id, ok := wl.lowerAssign(b, frontier); ok {
					next = append(next, id)
				}
			}
			if len(next) > 0 {
				frontier = next
			}
		case *lang.IfStmt, *lang.ForStmt:
			// Control flow does not become a WorkflowStep field (ADR 002 §4); it
			// lowers to the execution IR (LowerExec). The resource projection
			// instead FLATTENS every reachable arm/body into steps so the effect
			// bound computed over it (internal/effects walks these steps) is the
			// union over all branches — a conditional cannot smuggle an
			// unpermitted effect past the effects clause (ADR 002 §5, #199). The
			// flattened steps are a sound over-approximation for effect analysis
			// and plan diffing, not an independently executable graph; execution
			// runs from the execution IR.
			wl.lowerControlStmts([]lang.Stmt{st}, frontier)
		case *lang.ReturnStmt:
			wl.output = &spec.WorkflowOutput{
				Value: map[string]any{"value": wl.lowerValue(s.Value, "return", frontier)},
			}
		}
	}
}

// lowerControlStmts flattens the statements inside a conditional or loop into
// resource steps (the union over branches; see the IfStmt/ForStmt case in
// lowerBody). Every call becomes a step with a FRESH id — a name bound in both
// arms of an `if` must not collide — so these steps carry structural ids, not
// author binding names, and the projection is not required to be independently
// executable. Effect analysis only reads each step's uses:/agent/workflow, which
// this preserves faithfully.
func (wl *workflowLowerer) lowerControlStmts(body []lang.Stmt, predNeeds []string) {
	for _, st := range body {
		switch s := st.(type) {
		case *lang.AssignStmt:
			wl.lowerControlAssign(s, predNeeds)
		case *lang.ExprStmt:
			if call, ok := s.X.(*lang.CallExpr); ok {
				id := wl.freshID(calleeLeaf(call.Callee))
				wl.lowerCall(id, call, predNeeds, s.Pos)
			} else {
				wl.l.diag(s.Pos, "expression statement is not a call and has no effect")
			}
		case *lang.ParallelStmt:
			for _, b := range s.Body {
				wl.lowerControlAssign(b, predNeeds)
			}
		case *lang.IfStmt:
			wl.lowerControlStmts(s.Then, predNeeds)
			wl.lowerControlStmts(s.Else, predNeeds)
		case *lang.ForStmt:
			if v := identName(s.Var); v != "" {
				// Register the loop variable so references to it inside the body
				// resolve (best-effort) instead of being flagged unresolved.
				wl.env.roots[v] = "loop." + v
			}
			wl.lowerControlStmts(s.Body, predNeeds)
		case *lang.ReturnStmt:
			wl.output = &spec.WorkflowOutput{
				Value: map[string]any{"value": wl.lowerValue(s.Value, "return", predNeeds)},
			}
		}
	}
}

// lowerControlAssign lowers an assignment inside a control-flow body, allocating
// a fresh step id (so branch-local bindings never collide) and registering the
// binding's interpolation root best-effort.
func (wl *workflowLowerer) lowerControlAssign(s *lang.AssignStmt, predNeeds []string) {
	name := identName(s.Target)
	switch v := s.Value.(type) {
	case *lang.CallExpr:
		id := wl.freshID(name)
		wl.lowerCall(id, v, predNeeds, s.Pos)
		wl.env.roots[name] = "steps." + id + ".output"
	case *lang.RefExpr:
		wl.env.roots[name] = wl.prefixOf(v)
	case *lang.LitExpr:
		// A literal binding contributes no step and no interpolation root.
	default:
		wl.l.diag(s.Pos, "unsupported binding value")
	}
}

// lowerAssign lowers `target = <call>` (or `target = <ref>` alias) and returns
// the produced step id, or ok=false when no step is produced (an alias).
func (wl *workflowLowerer) lowerAssign(s *lang.AssignStmt, predNeeds []string) (string, bool) {
	name := identName(s.Target)
	switch v := s.Value.(type) {
	case *lang.CallExpr:
		wl.lowerCall(name, v, predNeeds, s.Pos)
		wl.env.roots[name] = "steps." + name + ".output"
		return name, true
	case *lang.RefExpr:
		// An alias binds a name to an existing value; it produces no step.
		wl.env.roots[name] = wl.prefixOf(v)
		return "", false
	default:
		wl.l.diag(s.Pos, "unsupported binding value")
		return "", false
	}
}

// lowerCall lowers one call into a step, hoisting nested-call arguments into
// their own SSA-temporary steps first, and records the step's needs.
func (wl *workflowLowerer) lowerCall(id string, call *lang.CallExpr, predNeeds []string, pos spec.Pos) {
	step := spec.WorkflowStep{ID: id, Pos: pos, NeedsDeclared: true}
	wl.applyCallee(&step, call.Callee)

	var tempNeeds []string
	if len(call.Args) > 0 {
		with := make(map[string]any, len(call.Args))
		for i, arg := range call.Args {
			key := "arg" + strconv.Itoa(i)
			if arg.Name != nil && arg.Name.Name != "" {
				key = arg.Name.Name
			}
			with[key] = wl.lowerArg(arg.Value, id, i, predNeeds, &tempNeeds)
		}
		step.With = with
	}
	step.Needs = mergeNeeds(predNeeds, tempNeeds)
	wl.steps = append(wl.steps, step)
	// Anchor the step's source-map entry at the callee (invocation site) — the
	// position downstream ref/policy diagnostics underline (AgentPos/UsesPos),
	// falling back to the statement position.
	anchor := pos
	if call.Callee != nil && !call.Callee.Pos.IsZero() {
		anchor = call.Callee.Pos
	}
	wl.l.sm.set(KeyStep(wl.wf, id), anchor)
}

// applyCallee sets exactly one target field. A dotted callee (github.get_pr) is a
// tool call; a single identifier is a workflow: step when classifyDecls marked it
// a workflow (declared in this file or listed in Options.Workflows), otherwise an
// agent: step.
func (wl *workflowLowerer) applyCallee(step *spec.WorkflowStep, callee *lang.RefExpr) {
	if callee == nil || len(callee.Parts) == 0 {
		wl.l.diag(step.Pos, "call has no callee")
		return
	}
	if len(callee.Parts) >= 2 {
		tool := callee.Parts[0].Name
		ops := make([]string, 0, len(callee.Parts)-1)
		for _, p := range callee.Parts[1:] {
			ops = append(ops, p.Name)
		}
		step.Uses = "tool." + tool + "." + strings.Join(ops, ".")
		step.UsesPos = callee.Pos
		return
	}
	name := callee.Parts[0].Name
	if wl.l.workflowCallee[name] {
		step.Workflow = name
		step.WorkflowPos = callee.Pos
		return
	}
	step.Agent = name
	step.AgentPos = callee.Pos
}

// lowerArg lowers one argument value. A nested call is hoisted into an SSA
// temporary step whose id is the parent id plus the argument path (structural,
// never source-derived), referenced by ${steps.<temp>.output}.
func (wl *workflowLowerer) lowerArg(e lang.Expr, parentID string, argIdx int, predNeeds []string, tempNeeds *[]string) any {
	switch v := e.(type) {
	case *lang.RefExpr:
		return wl.token(v)
	case *lang.LitExpr:
		// A literal argument lowers to its Go value directly; the with: map holds
		// arbitrary values, so no interpolation token is needed (#199).
		return v.Value
	case *lang.CallExpr:
		id := wl.freshID(parentID + "_arg" + strconv.Itoa(argIdx))
		wl.lowerCall(id, v, predNeeds, v.Pos)
		*tempNeeds = append(*tempNeeds, id)
		return stepToken(id)
	default:
		wl.l.diag(e.Position(), "unsupported argument expression")
		return ""
	}
}

// lowerValue lowers a return/top-level expression, hoisting a call the same way
// lowerArg does.
func (wl *workflowLowerer) lowerValue(e lang.Expr, idBase string, predNeeds []string) any {
	switch v := e.(type) {
	case *lang.RefExpr:
		return wl.token(v)
	case *lang.LitExpr:
		return v.Value
	case *lang.CallExpr:
		id := wl.freshID(idBase + "_" + calleeLeaf(v.Callee))
		wl.lowerCall(id, v, predNeeds, v.Pos)
		return stepToken(id)
	default:
		wl.l.diag(e.Position(), "unsupported expression")
		return ""
	}
}

// token renders a reference as an interpolation string, e.g. result.summary ->
// ${steps.result.output.summary}, input.repo -> ${input.repo}.
//
// A bare reference to a single-parameter workflow's input resolves to the path
// "input" alone, and the resource-model interpolation language has no token for
// the whole workflow input — engine.resolvePath requires input.<field> (a step's
// whole output is reachable as ${steps.<id>.output}, but the input root is not).
// ${input} would skip-pass validation and fail-closed at run time, so it is
// reported as a diagnostic. The best-effort ${input} is STILL returned: like a
// duplicate name, an invalid token lives in a Result that carries diagnostics
// (see the Result doc), and a caller must check diagnostics before use rather
// than trust that only-valid IR was produced. Whole-input pass-through (handing a
// subworkflow its entire input) additionally needs a callee input-document
// mapping; both are follow-ups (docs/plans/197-lowering.md), not the resource
// projection's job.
func (wl *workflowLowerer) token(r *lang.RefExpr) any {
	path := wl.prefixOf(r)
	if path == "input" {
		wl.l.diag(r.Position(), "cannot reference the whole workflow input; use input.<field> (the resource model has no interpolation token for the entire input)")
	}
	return "${" + path + "}"
}

// prefixOf resolves a reference to its interpolation path (no ${}). An unresolved
// leading identifier yields a diagnostic and a best-effort literal path.
func (wl *workflowLowerer) prefixOf(r *lang.RefExpr) string {
	if r == nil || len(r.Parts) == 0 {
		return ""
	}
	head := r.Parts[0].Name
	prefix, ok := wl.env.roots[head]
	if !ok {
		wl.l.diag(r.Pos, "unresolved reference %q", head)
		prefix = head
	}
	rest := r.Parts[1:]
	if len(rest) == 0 {
		return prefix
	}
	parts := make([]string, 0, len(rest)+1)
	parts = append(parts, prefix)
	for _, p := range rest {
		parts = append(parts, p.Name)
	}
	return strings.Join(parts, ".")
}

// freshID returns base if unused, else base_1, base_2, … — deterministic in
// source order and independent of source location.
func (wl *workflowLowerer) freshID(base string) string {
	if base == "" {
		base = "step"
	}
	if _, ok := wl.used[base]; !ok {
		wl.used[base] = 0
		return base
	}
	for {
		wl.used[base]++
		cand := base + "_" + strconv.Itoa(wl.used[base])
		if _, ok := wl.used[cand]; !ok {
			wl.used[cand] = 0
			return cand
		}
	}
}

// --- helpers ----------------------------------------------------------------

func identName(id *lang.Ident) string {
	if id == nil {
		return ""
	}
	return id.Name
}

func calleeLeaf(callee *lang.RefExpr) string {
	if callee == nil || len(callee.Parts) == 0 {
		return "call"
	}
	return callee.Parts[len(callee.Parts)-1].Name
}

func stepToken(id string) string { return "${steps." + id + ".output}" }

// mergeNeeds concatenates predecessor and temp needs, de-duplicating while
// preserving first-seen order.
func mergeNeeds(pred, temp []string) []string {
	if len(pred) == 0 && len(temp) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(pred)+len(temp))
	out := make([]string, 0, len(pred)+len(temp))
	for _, group := range [][]string{pred, temp} {
		for _, id := range group {
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}
