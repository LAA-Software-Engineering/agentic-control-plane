// Package execir defines the execution IR: the derived, never-authored
// projection of a checked .agent program where control flow lives (ADR 002 §5,
// issue #199).
//
// ADR 002 fixes two sibling projections of one checked program. The resource
// projection (internal/spec resources) is what humans author, what `plan`
// diffs, and what `apply` writes; it never acquires an expression language, so
// `Branch` and `Loop` may not appear on a spec.WorkflowStep (ADR 002 §4). The
// execution IR — this package — is the other projection: it holds
// InvokeTool/InvokeAgent/InvokeWorkflow/Fork/Branch/Loop/Return and is where a
// conditional or a loop becomes something after the AST is discarded. It is
// never hand-authored and has no YAML surface.
//
// The two projections are NOT a pipeline: the execution IR is lowered directly
// from the checked program (internal/lang/lower.LowerExec), not derived from the
// resource projection, which by design cannot represent control flow.
//
// Ingress convergence is the DESIGN target, not yet the implementation. ADR 002
// §5 requires both ingress paths to converge on this IR — a straight-line YAML
// workflow lowering to the same flat node list a straight-line `.agent` workflow
// does, so execution semantics never diverge. This package provides the target
// and the `.agent` lowering (LowerExec); the YAML side still executes as a
// WorkflowStep DAG in internal/engine, and a YAML->execir lowering plus running
// the engine from execir is a follow-up. Until that lands, do not read this
// package as proof the two paths already share an interpreter.
//
// Runtime independence: nodes reference values by the source binding namespace
// (parameter names, assignment targets, loop variables), not by resource-model
// interpolation tokens such as ${steps.x.output}. An [Interp] executes a Program
// against a [Scope] and an injected [Invoker]; the package deliberately does not
// depend on the engine, state, or model registry, so control-flow semantics are
// unit-testable in isolation (issue #199, library-level scope).
package execir

import "github.com/LAA-Software-Engineering/terfyn/internal/spec"

// Pos aliases spec.Pos so an execution-IR node can carry the same position a
// lang AST node did, with no conversion (mirrors lang.Pos).
type Pos = spec.Pos

// Program is the execution lowering of one workflow: its parameter names, the
// ordered top-level nodes to execute, and a canonical digest that folds into the
// workflow hash (ADR 002 §5; see [Program.Digest] and internal/plan).
type Program struct {
	Workflow string
	Params   []string
	Body     []Node
}

// Node is one execution-IR construct.
type Node interface{ node() }

// InvokeTool is a deterministic workflow-level tool call `uses(args)`. Bind is
// the source binding name the result is stored under, or "" when the call is
// evaluated only for its effect (a bare-expression statement).
type InvokeTool struct {
	Pos  Pos
	Bind string
	Uses string
	Args map[string]Value
}

func (*InvokeTool) node() {}

// InvokeAgent invokes a declared agent. Bind is the result binding, or "".
type InvokeAgent struct {
	Pos   Pos
	Bind  string
	Agent string
	Args  map[string]Value
}

func (*InvokeAgent) node() {}

// InvokeWorkflow invokes a declared subworkflow. Bind is the result binding, or "".
type InvokeWorkflow struct {
	Pos      Pos
	Bind     string
	Workflow string
	Args     map[string]Value
}

func (*InvokeWorkflow) node() {}

// Let binds a name to a value without invoking anything — the lowering of an
// alias assignment `x = y`.
type Let struct {
	Pos   Pos
	Bind  string
	Value Value
}

func (*Let) node() {}

// Branch is a conditional: run Then when Cond evaluates truthy, else Else. Else
// may be empty. Branch lives only in the execution IR (ADR 002 §4); the resource
// projection instead flattens both arms into steps so the effect bound is the
// union over branches.
type Branch struct {
	Pos  Pos
	Cond Expr
	Then []Node
	Else []Node
}

func (*Branch) node() {}

// ForkBranch is one statically-known parallel branch: a name it binds and the
// nodes it runs.
type ForkBranch struct {
	Bind  string
	Nodes []Node
}

// Fork runs statically-known branches concurrently and joins at its end — the
// lowering of `parallel { a = ...; b = ... }` (#192). The join is the implicit
// barrier at the close of the node: every branch completes before the node that
// follows the Fork begins.
type Fork struct {
	Pos      Pos
	Branches []ForkBranch
}

func (*Fork) node() {}

// Loop iterates Body once per element of Collection with Var bound to the
// element. Parallel marks dynamic fan-out — iterations run with bounded
// concurrency (ADR 002 §1: dynamic fan-out is a loop, not a graph field).
//
// Scope rules differ by kind, and match the type checker:
//
//   - A SEQUENTIAL Loop runs its iterations in order on the ENCLOSING scope. The
//     loop variable and any body binding escape the loop (last iteration wins),
//     and a Return in the body returns from the workflow and stops the loop.
//   - A PARALLEL Loop runs each iteration in an ISOLATED child scope, so
//     iterations never race and no body binding escapes; `return` is not lowered
//     into a parallel body (LowerExec rejects it), so isolation loses nothing.
//
// There is no unbounded (`while`) loop in the surface, and the interpreter caps
// the iteration count (internal/spec MaxLoopIterations) so termination is always
// bounded (#199).
type Loop struct {
	Pos        Pos
	Var        string
	Collection Value
	Body       []Node
	Parallel   bool
}

func (*Loop) node() {}

// Return sets the workflow output value.
type Return struct {
	Pos   Pos
	Value Value
}

func (*Return) node() {}

// --- Values -----------------------------------------------------------------

// Value is a data operand: a [Ref] into the runtime scope or a [Lit].
type Value interface{ value() }

// Ref is a dotted path into the runtime scope, e.g. ["pr","number"] resolving
// scope["pr"] then its "number" field, or ["input","repo"], or a loop variable.
// Path is always non-empty.
type Ref struct {
	Pos  Pos
	Path []string
}

func (Ref) value() {}

// Lit is a literal operand: a string, int64, float64, or bool.
type Lit struct {
	Pos Pos
	V   any
}

func (Lit) value() {}

// --- Condition expressions --------------------------------------------------

// Expr is a boolean condition tree.
type Expr interface{ expr() }

// Leaf wraps a Value (Ref or Lit) as a condition leaf.
type Leaf struct{ V Value }

func (Leaf) expr() {}

// Not is logical negation.
type Not struct{ X Expr }

func (Not) expr() {}

// BinOp is a comparison or logical connective. Op is one of:
// "==", "!=", "<", "<=", ">", ">=", "&&", "||".
type BinOp struct {
	Op   string
	X, Y Expr
}

func (BinOp) expr() {}
