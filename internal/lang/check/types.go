package check

import (
	"fmt"
	"path/filepath"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/lang"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/schema"
)

// agentTypeInfo is the resolved input/output schema for one agent declared in
// the compilation unit (f or Options.Files). A nil field is untyped — either
// the .agent source omitted the field, or no schemas/<Name>.json file exists
// for the declared type name (gradual typing: an unresolved type checks as
// untyped rather than failing).
type agentTypeInfo struct {
	Input, Output *schema.Document
}

// workflowTypeInfo is the resolved param/result schema for one workflow.
// ParamOrder preserves declaration order so a positional call argument can be
// matched to the right parameter even without a named arg.
type workflowTypeInfo struct {
	ParamOrder []string
	Params     map[string]*schema.Document
	Result     *schema.Document
}

// typeUniverse resolves every TypeRef reachable from the compilation unit
// against the schemas/<Name>.json convention (docs/plans/198-type-effect-checking.md
// design decision 4 — a new naming rule with no prior ADR text, flagged there
// for review). Only agents and workflows declared in this compilation unit
// (f plus Options.Files) get resolved types; a callee that resolves only
// through Options.Project (a YAML-only sibling) is treated as untyped here —
// wiring full YAML schema interop into this checker is a follow-up, not
// silently assumed (see docs/plans/198-type-effect-checking.md §8).
type typeUniverse struct {
	dir       string
	agents    map[string]agentTypeInfo
	workflows map[string]workflowTypeInfo
}

func resolveTypes(f *lang.File, opts Options) *typeUniverse {
	dir := opts.SchemaDir
	if dir == "" {
		dir = schemaDirFor(f)
	}
	tu := &typeUniverse{dir: dir, agents: map[string]agentTypeInfo{}, workflows: map[string]workflowTypeInfo{}}

	files := make([]*lang.File, 0, len(opts.Files)+1)
	files = append(files, f)
	files = append(files, opts.Files...)

	for _, file := range files {
		if file == nil {
			continue
		}
		for _, d := range file.Decls {
			switch decl := d.(type) {
			case *lang.AgentDecl:
				name := identName(decl.Name)
				if name == "" {
					continue
				}
				tu.agents[name] = agentTypeInfo{
					Input:  tu.resolve(decl.Input),
					Output: tu.resolve(decl.Output),
				}
			case *lang.WorkflowDecl:
				name := identName(decl.Name)
				if name == "" {
					continue
				}
				order := make([]string, 0, len(decl.Params))
				params := make(map[string]*schema.Document, len(decl.Params))
				for _, p := range decl.Params {
					pn := identName(p.Name)
					order = append(order, pn)
					params[pn] = tu.resolve(p.Type)
				}
				tu.workflows[name] = workflowTypeInfo{
					ParamOrder: order,
					Params:     params,
					Result:     tu.resolve(decl.Result),
				}
			}
		}
	}
	return tu
}

// resolve loads t's backing schema document, or nil (untyped) if t is absent
// or no schemas/<Name>.json file exists.
func (tu *typeUniverse) resolve(t *lang.TypeRef) *schema.Document {
	if t == nil || t.Name == "" {
		return nil
	}
	path := filepath.Join(tu.dir, "schemas", t.Name+".json")
	doc, err := schema.LoadDocument(path)
	if err != nil {
		return nil
	}
	return doc
}

func schemaDirFor(f *lang.File) string {
	if f == nil || f.Pos.File == "" {
		return "."
	}
	return filepath.Dir(f.Pos.File)
}

// typeRef is a value's resolved type: a root schema.Document plus the dotted
// path walked so far. A nil doc means untyped (gradual typing — always
// compatible). Kept as (doc, path) rather than eagerly resolving to a TypeSet
// so a chain like result.summary can extend the path one field at a time,
// mirroring how internal/spec/wiring.go walks interpolation paths against the
// same schema.Document API, but over AST RefExpr.Parts instead of a regex over
// an interpolation string.
type typeRef struct {
	doc  *schema.Document
	path []string
}

func (t typeRef) types() schema.TypeSet {
	if t.doc == nil {
		return nil
	}
	return t.doc.Lookup(t.path).Types
}

func (t typeRef) child(field string) (typeRef, schema.LookupResult) {
	if t.doc == nil {
		return typeRef{}, schema.LookupResult{}
	}
	p := make([]string, 0, len(t.path)+1)
	p = append(p, t.path...)
	p = append(p, field)
	res := t.doc.Lookup(p)
	return typeRef{doc: t.doc, path: p}, res
}

// checkTypes type-checks agent invocation arguments and value flow between
// bindings for every workflow in f (docs/plans/198-type-effect-checking.md
// design decision 5).
func checkTypes(f *lang.File, tu *typeUniverse) lang.Diagnostics {
	var diags lang.Diagnostics
	for _, d := range f.Decls {
		wd, ok := d.(*lang.WorkflowDecl)
		if !ok {
			continue
		}
		diags = append(diags, checkWorkflowTypes(wd, tu)...)
	}
	return diags
}

type wfChecker struct {
	tu  *typeUniverse
	wf  *lang.WorkflowDecl
	env map[string]typeRef
}

func checkWorkflowTypes(wd *lang.WorkflowDecl, tu *typeUniverse) lang.Diagnostics {
	wc := &wfChecker{tu: tu, wf: wd, env: map[string]typeRef{}}
	info := tu.workflows[identName(wd.Name)]
	for _, p := range wd.Params {
		name := identName(p.Name)
		if name == "" {
			continue
		}
		wc.env[name] = typeRef{doc: info.Params[name]}
	}
	var diags lang.Diagnostics
	for _, st := range wd.Body {
		diags = append(diags, wc.checkStmt(st)...)
	}
	return diags
}

func (wc *wfChecker) checkStmt(st lang.Stmt) lang.Diagnostics {
	switch s := st.(type) {
	case *lang.AssignStmt:
		t, diags := wc.checkExpr(s.Value)
		wc.env[identName(s.Target)] = t
		return diags
	case *lang.ExprStmt:
		_, diags := wc.checkExpr(s.X)
		return diags
	case *lang.ParallelStmt:
		var diags lang.Diagnostics
		results := make(map[string]typeRef, len(s.Body))
		for _, b := range s.Body {
			t, d := wc.checkExpr(b.Value)
			diags = append(diags, d...)
			results[identName(b.Target)] = t
		}
		for name, t := range results {
			wc.env[name] = t
		}
		return diags
	case *lang.ReturnStmt:
		got, diags := wc.checkExpr(s.Value)
		want := typeRef{doc: wc.tu.workflows[identName(wc.wf.Name)].Result}
		diags = append(diags, wc.checkCompatible(s.Value.Position(), got, want, "return value")...)
		return diags
	}
	return nil
}

func (wc *wfChecker) checkExpr(e lang.Expr) (typeRef, lang.Diagnostics) {
	switch v := e.(type) {
	case *lang.RefExpr:
		return wc.checkRef(v)
	case *lang.CallExpr:
		return wc.checkCall(v)
	}
	return typeRef{}, nil
}

// checkRef resolves a dotted reference (pr, input.repo, result.summary)
// against the environment, reporting a diagnostic only when the schema
// positively forbids the path (additionalProperties: false); an untyped head
// or an unresolved head (already lowering's diagnostic — see prefixOf in
// internal/lang/lower/lower.go) is silently gradual here.
func (wc *wfChecker) checkRef(r *lang.RefExpr) (typeRef, lang.Diagnostics) {
	if r == nil || len(r.Parts) == 0 {
		return typeRef{}, nil
	}
	head := r.Parts[0].Name
	cur, ok := wc.env[head]
	if !ok {
		return typeRef{}, nil
	}
	for _, p := range r.Parts[1:] {
		if cur.doc == nil {
			return typeRef{}, nil
		}
		next, res := cur.child(p.Name)
		if res.Missing {
			return typeRef{}, lang.Diagnostics{{
				Pos: r.Pos,
				Msg: fmt.Sprintf("%q is not declared in the schema for %q", p.Name, head),
			}}
		}
		cur = next
	}
	return cur, nil
}

// checkCall type-checks a call's arguments against its callee's declared
// parameter/input types (when the callee is declared in this compilation
// unit) and returns the call's own result type.
func (wc *wfChecker) checkCall(c *lang.CallExpr) (typeRef, lang.Diagnostics) {
	var diags lang.Diagnostics
	if c.Callee == nil || len(c.Callee.Parts) == 0 {
		return typeRef{}, nil
	}
	if len(c.Callee.Parts) >= 2 {
		// A dotted callee is a tool call. There is no .agent-visible tool
		// schema, so an argument's own internal well-formedness is still
		// checked (nested calls, member access), but not compatibility against
		// a callee parameter — gradual typing.
		for _, arg := range c.Args {
			_, d := wc.checkExpr(arg.Value)
			diags = append(diags, d...)
		}
		return typeRef{}, diags
	}
	name := c.Callee.Parts[0].Name
	if wi, ok := wc.tu.workflows[name]; ok {
		diags = append(diags, wc.checkWorkflowArgs(name, wi, c)...)
		return typeRef{doc: wi.Result}, diags
	}
	if ai, ok := wc.tu.agents[name]; ok {
		diags = append(diags, wc.checkAgentArgs(name, ai, c)...)
		return typeRef{doc: ai.Output}, diags
	}
	// Not declared in this compilation unit: a YAML-only sibling or simply
	// undeclared (spec.ValidateProjectGraph reports the latter after lowering).
	// Gradual typing: only check argument well-formedness.
	for _, arg := range c.Args {
		_, d := wc.checkExpr(arg.Value)
		diags = append(diags, d...)
	}
	return typeRef{}, diags
}

// checkWorkflowArgs matches call arguments to the callee's declared,
// ORDERED parameters — named args by name, positional args by position — and
// checks each against the parameter's resolved type.
func (wc *wfChecker) checkWorkflowArgs(name string, wi workflowTypeInfo, c *lang.CallExpr) lang.Diagnostics {
	var diags lang.Diagnostics
	pos := 0
	for _, arg := range c.Args {
		argType, d := wc.checkExpr(arg.Value)
		diags = append(diags, d...)

		paramName := ""
		if arg.Name != nil {
			paramName = arg.Name.Name
		} else if pos < len(wi.ParamOrder) {
			paramName = wi.ParamOrder[pos]
			pos++
		}
		if paramName == "" {
			continue
		}
		paramDoc, known := wi.Params[paramName]
		if !known {
			continue // an argument name with no matching parameter is not a type error
		}
		diags = append(diags, wc.checkCompatible(arg.Position(), argType, typeRef{doc: paramDoc},
			fmt.Sprintf("argument %q of %s", paramName, name))...)
	}
	return diags
}

// checkAgentArgs checks only the unambiguous case: a single positional
// argument standing for the agent's whole declared input. An agent's `input`
// is one type, not a named parameter list, so multiple positional or named
// arguments have no declared per-field mapping yet — lowering placeholder-keys
// them arg0, arg1, ... pending a rebind (docs/plans/198-type-effect-checking.md
// design decision 5, and internal/lang/lower/lower.go's own "#198 rebinds"
// note). Checking those against a guess would produce false positives, so
// they are left gradual rather than guessed.
func (wc *wfChecker) checkAgentArgs(name string, ai agentTypeInfo, c *lang.CallExpr) lang.Diagnostics {
	var diags lang.Diagnostics
	for _, arg := range c.Args {
		_, d := wc.checkExpr(arg.Value)
		diags = append(diags, d...)
	}
	if len(c.Args) == 1 && c.Args[0].Name == nil {
		argType, _ := wc.checkExpr(c.Args[0].Value)
		diags = append(diags, wc.checkCompatible(c.Args[0].Position(), argType, typeRef{doc: ai.Input},
			fmt.Sprintf("input of %s", name))...)
	}
	return diags
}

func (wc *wfChecker) checkCompatible(pos lang.Pos, got, want typeRef, what string) lang.Diagnostics {
	gotTypes, wantTypes := got.types(), want.types()
	if len(gotTypes) == 0 || len(wantTypes) == 0 {
		return nil // gradual typing: an untyped side is always compatible
	}
	if schema.Compatible(gotTypes, wantTypes) {
		return nil
	}
	return lang.Diagnostics{{
		Pos: pos,
		Msg: fmt.Sprintf("%s: type %s is not compatible with declared type %s", what, gotTypes, wantTypes),
	}}
}
