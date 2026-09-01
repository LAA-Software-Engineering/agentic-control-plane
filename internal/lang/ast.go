package lang

// The typed AST for the .agent surface (ADR 002). Every node implements Node
// and carries a spec.Pos (aliased as Pos) at its start. The tree records the
// declarations as written, not what they resolve to — reference resolution,
// typing, and effect checking are #198, and lowering to the resource model is
// #197. A construct the grammar admits at most once (an agent field) is
// reported as a diagnostic if repeated rather than silently overwritten, so no
// written declaration is dropped without a diagnostic.

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

// AgentDecl is `agent <Name> { ... }`. Each field appears at most once; a field
// the author omitted is a nil pointer (requiredness is a checking concern, #198)
// and a repeated field keeps the first occurrence and yields a duplicate-field
// diagnostic (the grammar admits each field once). Fields do not preserve
// source order across kinds — the surface fixes their meaning by keyword, not
// position.
type AgentDecl struct {
	Pos          Pos
	Name         *Ident
	Model        *ModelRef  // model <provider>/<name>
	Policy       *Ident     // policy <name> (reference to a Policy resource)
	Instructions *StringLit // instructions "..." (the agent prompt; lowers to AgentSpec.Instructions)
	Grants       []*Grant   // grants { tool.<name>.<operation> ... }
	Input        *TypeRef   // input <Type>
	Output       *TypeRef   // output <Type>
}

func (d *AgentDecl) Position() Pos { return d.Pos }
func (d *AgentDecl) declNode()     {}

// StringLit is a string literal occurrence with its position and decoded value.
// It carries the same value whether the source used a single-quoted `"..."` or a
// triple-quoted `"""..."""` literal (the multiline form is an ordinary string, not
// a distinct AST type); the lexer has already normalized a multiline body.
type StringLit struct {
	Pos   Pos
	Value string
}

func (s *StringLit) Position() Pos { return s.Pos }

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
// tool.<Name>.<Operation> — the exact reference vocabulary of
// approvals.requiredFor / uses: — and lives in a namespace distinct from
// effects. It is split the same way as tools.ParseUses: the leading "tool"
// segment is the namespace marker, Name is the single tool-name segment, and
// Operation is everything after it. Operation is therefore a dotted path, not a
// single identifier: shipped strings such as tool.github.pull_request.get carry
// a multi-segment operation (pull_request.get), and a lowering pass (#197)
// reconstructs the uses string as tool.<Name>.<Operation joined by ".">.
// A grant that omits the tool. prefix is a diagnostic, not an EffectRef.
type Grant struct {
	Pos  Pos
	Name *Ident // the <Name> segment (tool.<Name>.<operation>); a single identifier
	// Operation is the operation path (the segments after the tool name), at
	// least one identifier and possibly dotted (pull_request.post_comment).
	Operation []*Ident
	// Segments is the full dotted path as written (including the leading
	// "tool"), preserved for diagnostics and round-tripping.
	Segments []*Ident
}

func (g *Grant) Position() Pos { return g.Pos }

// ToolName returns the granted tool's name, or "" if the grant is malformed.
func (g *Grant) ToolName() string {
	if g.Name == nil {
		return ""
	}
	return g.Name.Name
}

// OperationName returns the dotted operation path (e.g. "pull_request.get"), or
// "" if the grant is malformed. This is the <operation> half that ParseUses
// yields and that a uses string reconstructs after the tool name.
func (g *Grant) OperationName() string { return dottedName(g.Operation) }

// --- Workflow declarations --------------------------------------------------

// WorkflowDecl is `workflow <Name>(<params>) -> <Result> effects { ... } { body }`.
// Result and the effects clause are optional in the grammar; the body is a
// statement list that may include conditionals and loops (IfStmt, ForStmt; #199)
// in addition to assignments, calls, parallel blocks, and a return.
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

// IfStmt is `if <Cond> { <Then> } (else ({ <Else> } | <IfStmt>))?` (#199). Cond
// is a boolean expression; Then and Else are statement lists. An `else if` chain
// parses as an Else holding a single nested IfStmt. Control flow never becomes a
// field on the resource-model WorkflowStep (ADR 002 §4): it lowers to the
// execution IR's Branch, and its two arms both flatten into the resource
// projection so the effect bound is the union over branches (ADR 002 §5).
type IfStmt struct {
	Pos  Pos
	Cond Expr
	Then []Stmt
	Else []Stmt
}

func (s *IfStmt) Position() Pos { return s.Pos }
func (s *IfStmt) stmtNode()     {}

// ForStmt is `for <Var> in <In> { <Body> }` (#199): iteration over a runtime
// collection. Parallel marks the dynamic fan-out form `parallel for <Var> in
// <In> { }` — ADR 002 §1 classifies dynamic fan-out over a runtime collection as
// "a loop wearing a graph costume," so it is language work, not a graph field.
// Both forms lower to the execution IR's Loop; only Parallel runs its iterations
// with bounded concurrency. Var binds inside Body only.
type ForStmt struct {
	Pos      Pos
	Var      *Ident
	In       Expr
	Body     []Stmt
	Parallel bool
}

func (s *ForStmt) Position() Pos { return s.Pos }
func (s *ForStmt) stmtNode()     {}

// --- Expressions ------------------------------------------------------------

// Expr is a workflow expression: a CallExpr, a RefExpr, or — in a condition or
// call argument (#199) — a LitExpr, UnaryExpr, or BinaryExpr.
type Expr interface {
	Node
	exprNode()
}

// LitExpr is a literal operand: a string, a number, or a boolean (#199). Kind is
// one of KindString, KindNumber, or a boolean (recorded as KindIdent with a
// bool Value). Value holds the decoded Go value: string, int64, float64, or
// bool. Literals appear in conditions and as call arguments; the surface has no
// arithmetic, so numbers are only ever compared or passed, never combined.
type LitExpr struct {
	Pos   Pos
	Kind  Kind
	Value any
}

func (e *LitExpr) Position() Pos { return e.Pos }
func (e *LitExpr) exprNode()     {}

// UnaryExpr is `<Op> <X>` — only `!` (logical negation) exists in the surface.
type UnaryExpr struct {
	Pos Pos
	Op  Kind // KindBang
	X   Expr
}

func (e *UnaryExpr) Position() Pos { return e.Pos }
func (e *UnaryExpr) exprNode()     {}

// BinaryExpr is `<X> <Op> <Y>`: a comparison (== != < <= > >=) or a logical
// connective (&& ||). Comparisons do not chain (a < b < c is a syntax error);
// logical connectives are left-associative with && binding tighter than ||.
type BinaryExpr struct {
	Pos Pos
	Op  Kind
	X   Expr
	Y   Expr
}

func (e *BinaryExpr) Position() Pos { return e.Pos }
func (e *BinaryExpr) exprNode()     {}

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
