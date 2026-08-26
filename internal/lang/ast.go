package lang

// The typed AST for the .agent surface (ADR 002). Every node implements Node
// and carries a spec.Pos (aliased as Pos) at its start. The tree is
// parse-faithful: it records what was written, not what it resolves to —
// reference resolution, typing, and effect checking are #198, and lowering to
// the resource model is #197.

// Node is any AST node. Position returns the node's start position.
type Node interface {
	Position() Pos
}

// File is the root: an ordered list of top-level declarations.
type File struct {
	Pos   Pos
	Decls []Decl
}

func (f *File) Position() Pos { return f.Pos }

// Decl is a top-level declaration: an AgentDecl or a WorkflowDecl.
type Decl interface {
	Node
	declNode()
}

// Ident is a bare identifier occurrence with its position.
type Ident struct {
	Pos  Pos
	Name string
}

func (i *Ident) Position() Pos { return i.Pos }

// --- Agent declarations -----------------------------------------------------

// AgentDecl is `agent <Name> { ... }`. Fields are optional at the parse layer;
// requiredness is a checking concern (#198), so a field the author omitted is a
// nil pointer rather than a parse error. Fields are populated in source order.
type AgentDecl struct {
	Pos    Pos
	Name   *Ident
	Model  *ModelRef // model <provider>/<name>
	Policy *Ident    // policy <name> (reference to a Policy resource)
	Grants []*Grant  // grants { tool.<name>.<operation> ... }
	Input  *TypeRef  // input <Type>
	Output *TypeRef  // output <Type>
}

func (d *AgentDecl) Position() Pos { return d.Pos }
func (d *AgentDecl) declNode()     {}

// ModelRef is a `<provider>/<name>` model reference such as openai/gpt-5.
// Provider and Name preserve hyphens (gpt-5); Raw is the reassembled text.
type ModelRef struct {
	Pos      Pos
	Provider string
	Name     string
	Raw      string
}

func (m *ModelRef) Position() Pos { return m.Pos }

// Grant is one autonomous capability bound inside a `grants { }` block. Per the
// ADR 002 amendment (#188) a grant names a concrete operation as
// tool.<Name>.<Operation> — the same reference vocabulary as
// approvals.requiredFor — and lives in a namespace distinct from effects. The
// parser records the leading "tool" segment so the distinction survives to the
// checker; a grant that omits the tool. prefix is a diagnostic, not an EffectRef.
type Grant struct {
	Pos       Pos
	Name      *Ident // the <Name> segment (tool.<Name>.<operation>)
	Operation *Ident // the <operation> segment
	// Segments is the full dotted path as written (including the leading
	// "tool"), preserved for diagnostics and round-tripping.
	Segments []*Ident
}

func (g *Grant) Position() Pos { return g.Pos }

// --- Workflow declarations --------------------------------------------------

// WorkflowDecl is `workflow <Name>(<params>) -> <Result> effects { ... } { body }`.
// Result and the effects clause are optional in the grammar; the body is a flat
// statement list (no conditionals or loops — those are #199).
type WorkflowDecl struct {
	Pos     Pos
	Name    *Ident
	Params  []*Param
	Result  *TypeRef     // return type after ->; nil if omitted
	Effects []*EffectRef // effects { github.read, ... }; nil if no clause
	Body    []Stmt
}

func (d *WorkflowDecl) Position() Pos { return d.Pos }
func (d *WorkflowDecl) declNode()     {}

// Param is one `<name>: <Type>` workflow parameter.
type Param struct {
	Pos  Pos
	Name *Ident
	Type *TypeRef
}

func (p *Param) Position() Pos { return p.Pos }

// TypeRef names a schema/type by identifier (e.g. PullRequest, Review). It is an
// unresolved reference at the parse layer.
type TypeRef struct {
	Pos  Pos
	Name string
}

func (t *TypeRef) Position() Pos { return t.Pos }

// EffectRef is one bare dotted effect identifier in the effects clause, such as
// github.read or external.visible. Unlike a Grant it carries no tool. prefix;
// the two namespaces must never be interchangeable (ADR 002).
type EffectRef struct {
	Pos  Pos
	Name string // dotted, e.g. "github.read"
}

func (e *EffectRef) Position() Pos { return e.Pos }

// --- Statements -------------------------------------------------------------

// Stmt is a workflow body statement.
type Stmt interface {
	Node
	stmtNode()
}

// AssignStmt is `<Target> = <Value>` binding a name to an expression result.
type AssignStmt struct {
	Pos    Pos
	Target *Ident
	Value  Expr
}

func (s *AssignStmt) Position() Pos { return s.Pos }
func (s *AssignStmt) stmtNode()     {}

// ExprStmt is a bare expression used for its effect, e.g. a deterministic tool
// call whose result is unbound: github.post_comment(...).
type ExprStmt struct {
	Pos Pos
	X   Expr
}

func (s *ExprStmt) Position() Pos { return s.Pos }
func (s *ExprStmt) stmtNode()     {}

// ParallelStmt is `parallel { <AssignStmt>... }` — static fan-out into named
// branches with fan-in (ADR 002 graph structure; #192). Each branch binds a
// name, so the body admits only assignments.
type ParallelStmt struct {
	Pos  Pos
	Body []*AssignStmt
}

func (s *ParallelStmt) Position() Pos { return s.Pos }
func (s *ParallelStmt) stmtNode()     {}

// ReturnStmt is `return <Value>`.
type ReturnStmt struct {
	Pos   Pos
	Value Expr
}

func (s *ReturnStmt) Position() Pos { return s.Pos }
func (s *ReturnStmt) stmtNode()     {}

// --- Expressions ------------------------------------------------------------

// Expr is a workflow expression: a CallExpr or a RefExpr.
type Expr interface {
	Node
	exprNode()
}

// RefExpr is a dotted reference path: a bare name (pr), a member access
// (input.repo, result.summary), or a callee path (github.get_pr). Parts holds
// each dotted segment in order and is always non-empty.
type RefExpr struct {
	Pos   Pos
	Parts []*Ident
}

func (e *RefExpr) Position() Pos { return e.Pos }
func (e *RefExpr) exprNode()     {}

// CallExpr is `<Callee>(<args>)`. Callee is the dotted reference being invoked
// (a workflow-level tool call like github.get_pr, or an agent/subworkflow
// invocation like SecurityReviewer). Args may nest arbitrarily.
type CallExpr struct {
	Pos    Pos
	Callee *RefExpr
	Args   []*Arg
}

func (e *CallExpr) Position() Pos { return e.Pos }
func (e *CallExpr) exprNode()     {}

// Arg is one call argument. Name is nil for a positional argument and set for a
// named one (repo: input.repo). A single call may mix positional and named
// arguments at the parse layer; validity is a checking concern (#198).
type Arg struct {
	Pos   Pos
	Name  *Ident // nil => positional
	Value Expr
}

func (a *Arg) Position() Pos { return a.Pos }
