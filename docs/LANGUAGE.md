# The `.agent` language — grammar reference

`.agent` is the surface syntax for authoring agents and workflows, fixed by
[ADR 002](adr/002-language-frontend-and-ir-expressiveness.md). This document is the
grammar reference for the **frontend as shipped in issue #196: lexing, parsing, and
the typed AST only** — plus the **resource-model lowering added in #197** (see
[Lowering to the resource model](#lowering-to-the-resource-model-197) below), **type and
effect checking, added in #198** (see [Type and effect checking](#type-and-effect-checking-198)
below), and **conditionals, loops, dynamic fan-out, and the execution IR, added in
#199** (see [Control flow and the execution IR](#control-flow-and-the-execution-ir-199)
below), and **CLI ingestion added in #200**: `project.LoadProject` discovers every `.agent`
file under the project root (skipping dot-directories) and compiles the set through the
checker ([`internal/lang/check`](../internal/lang/check)) — type and effect checking plus the
positional workflow-argument rebind — merging the CHECKED resource projection into the graph
that `validate`/`plan`/`apply`/`run` consume. `terfyn export --format yaml` materializes that
graph (ADR 003), `terfyn fmt` formats `.agent`, and `terfyn init` scaffolds a `.agent`
project.

**`.agent` workflows execute end-to-end, including control flow.** `if`/`else`, sequential
`for` (with `return`), and `parallel for` run through the **execution IR**
([`internal/execir`](../internal/execir)): the checker lowers each workflow to an
`execir.Program`, that program is pinned into the deployment snapshot (#260), and a
control-flow workflow runs on the `execir` interpreter — the taken arm only — rather than the
resource DAG (#259). The resource projection still flattens both arms of a conditional and a
loop body into steps, but only for **effect analysis** (`effects.Compute`), where the union
over arms is the sound bound; it is never executed for such a workflow. Straight-line, `needs`
DAGs, and `parallel { }` continue to run on the `WorkflowStep` DAG (retiring it in favor of one
`execir` run path is a follow-up, #278). See `examples/agent-control-flow`.

The reference implementation is [`internal/lang`](../internal/lang):
`lang.Parse(file, src) (*lang.File, lang.Diagnostics)`.

## Design notes

- **Positions.** Every token and every AST node carries a position that is the same
  type as the IR positions threaded through the resource model by #187
  (`lang.Pos = spec.Pos`, i.e. `{File, Line, Column}`, 1-based). A later lowering pass
  can copy an AST node's `Pos` onto an IR node with no conversion.
- **Error recovery.** The parser does not stop at the first error. After a diagnostic
  it synchronizes to the next declaration, statement, list element, or line and keeps
  going, so a malformed file yields every diagnostic it can find in one pass.
- **Grants vs. effects are distinct namespaces** (ADR 002 amendment, #188). A **grant**
  names a concrete operation as `tool.<name>.<operation>` — the exact reference
  vocabulary of `approvals.requiredFor` / `uses:`. It is split the same way as
  [`tools.ParseUses`](../internal/tools/registry.go): the tool name is the single segment
  after `tool.`, and the **operation is everything after it** and may be dotted
  (`tool.github.pull_request.post_comment` → tool `github`, operation
  `pull_request.post_comment`). An **effect** is a bare dotted identifier
  (`github.read`). The parser keeps them separate and rejects a grant written as a bare
  name or an effect written with a `tool.` prefix.
- **Deliberately omitted** (ADR 002): anonymous functions — every callable is a
  declared resource — and any call-site `approve` statement — approval scope lives in
  `Policy` resources.

## Lexical structure

- **Whitespace and newlines** are insignificant; they only separate tokens. The grammar
  is not newline-terminated — each construct has a deterministic shape.
- **Comments** run from `//` to the end of the line.
- **Identifiers** match `[A-Za-z_][A-Za-z0-9_-]*`. A hyphen is legal after the first
  character (the language has no arithmetic, so `-` is never an operator). This lets
  DNS-style resource references (`guarded-writes`) and model name segments (`gpt-5`) be
  single tokens.
- **Reserved words** — reserved because they always begin a construct and never name a
  thing in this surface: `agent`, `workflow`, `parallel`, `return`. The field words
  `model`, `policy`, `grants`, `input`, `output`, and the clause word `effects` are
  **contextual**: they are ordinary identifiers the parser recognizes by position, so
  they may also be used as parameter or binding names.
- **Punctuation**: `{` `}` `(` `)` `.` `/` `,` `:` `=` `->`.

## Grammar (EBNF)

```ebnf
File        = { Declaration } ;
Declaration = AgentDecl | WorkflowDecl ;

AgentDecl   = "agent" Ident "{" { AgentField } "}" ;
AgentField  = "model"  ModelRef
            | "policy" Ident
            | "grants" "{" { Grant } "}"
            | "input"  Ident
            | "output" Ident ;
ModelRef    = Ident "/" Ident ;                 (* e.g. openai/gpt-5 *)
Grant       = "tool" "." Ident "." Operation ;  (* tool.<name>.<operation> *)
Operation   = Ident { "." Ident } ;             (* name = first Ident, operation = the rest *)

WorkflowDecl = "workflow" Ident "(" [ Params ] ")" [ "->" Ident ]
               [ "effects" "{" [ Effects ] "}" ]
               "{" { Statement } "}" ;
Params      = Param { "," Param } ;
Param       = Ident ":" Ident ;                 (* name : Type *)
Effects     = Effect { [ "," ] Effect } ;       (* commas optional *)
Effect      = Ident { "." Ident } ;             (* bare dotted; no "tool." prefix *)

Statement   = Assign | Parallel | If | For | While | Return | ExprStmt ;
Assign      = Ident "=" Expr ;
Parallel    = "parallel" "{" { Assign } "}" ;   (* static fan-out, #192 *)
If          = "if" Cond Block [ "else" ( If | Block ) ] ;            (* #199 *)
For         = [ "parallel" ] "for" Ident "in" Expr Block ;           (* #199 *)
While       = "while" Cond "limit" Number Block ;                    (* bounded, #288 *)
Block       = "{" { Statement } "}" ;
Return      = "return" Expr ;
ExprStmt    = Expr ;                            (* a call for its effect *)

Expr        = Ref [ "(" [ Args ] ")" ] | Literal ;
Args        = Arg { "," Arg } ;
Arg         = [ Ident ":" ] Expr ;              (* named or positional *)
Ref         = Ident { "." Ident } ;             (* pr, input.repo, github.get_pr *)
Literal     = String | Number | "true" | "false" ;

(* Boolean expression language for conditions, #199. No arithmetic. *)
Cond        = Or ;
Or          = And { "||" And } ;
And         = Compare { "&&" Compare } ;
Compare     = Unary [ ( "==" | "!=" | "<" | "<=" | ">" | ">=" ) Unary ] ;  (* non-chaining *)
Unary       = "!" Unary | Primary ;
Primary     = Literal | "(" Cond ")" | Ref [ "(" [ Args ] ")" ] ;
```

Notes:

- The `effects` clause accepts commas between effects or none, so it reads the same
  whether comma- or newline-separated. Grants are newline-separated (a comma between
  grants is tolerated for symmetry).
- A `parallel` block admits only assignments: each branch binds a name for fan-in.
- `if`, `else`, `for`, and the operators/literals are the #199 additions. `in` is a
  **contextual** keyword (matched only in loop position), so a parameter may still be
  named `in`; `true`/`false` are contextual boolean literals. `parallel for` is dynamic
  fan-out — a loop, not a graph field (ADR 002 §1).
- `while <cond> limit N { … }` is the **bounded** loop (#288, ADR 002 §6). `limit` is a
  **contextual** keyword (a parameter may still be named `limit`) and the bound `N` is a
  **mandatory positive integer literal** — a missing, zero, fractional, or dynamic
  (`limit input.max`) bound is a diagnostic. There is no unbounded `while` and no
  `parallel while`. See [Bounded termination](#bounded-termination) for the semantics.
- **Conditions are pure**: a call is not allowed inside an `if`, a `while`, or a comparison
  operand. Bind a call's result to a name and test the name. This keeps conditions
  effect-free and the effect bound trivially the union over both arms.
- Comparisons do not chain: `a < b < c` is a syntax error (parenthesize or use `&&`).
- Each agent field (`model`, `policy`, `grants`, `input`, `output`) may appear at most
  once; a repeated field keeps the first occurrence and yields a duplicate-field
  diagnostic rather than silently overwriting.
- Requiredness of agent fields, argument style (all-positional vs. all-named), reference
  resolution, and effect soundness are **not** enforced by the parser — they are
  checking concerns (#198). A field the author omitted is a nil node, not a parse error.

## The normative program

The following is the ADR 002 target surface and the acceptance fixture — it parses to a
typed AST with no diagnostics
([`internal/lang/testdata/valid/adr002.agent`](../internal/lang/testdata/valid/adr002.agent)):

```agent
agent Reviewer {
    model  openai/gpt-5
    policy guarded-writes
    grants {
        tool.github.read_pr
        tool.github.read_comments
    }
    input  ReviewRequest
    output Review
}

workflow PRReview(input: PullRequest) -> Review
    effects { github.read, github.write, external.visible }
{
    pr = github.get_pr(input.repo, input.number)

    parallel {
        security = SecurityReviewer(pr)
        quality  = Reviewer(pr)
        tests    = TestReviewer(pr)
    }

    result = Synthesizer(security, quality, tests)

    github.post_comment(repo: input.repo, number: input.number, body: result.summary)
    return result
}
```

## Lowering to the resource model (#197)

[`internal/lang/lower`](../internal/lang/lower) turns the typed AST into the existing
resource model — the **resource projection** of [ADR 002](adr/002-language-frontend-and-ir-expressiveness.md)
§5 (`lower.LowerFile(f, lower.Options{}) (*lower.Result, lang.Diagnostics)`). This is the
desired-state view `plan` diffs and `apply` writes; it is a *sibling* of the execution
lowering (#199), not its input — control flow cannot be recovered from it, so the two are
independent projections of the checked program.

Lowering rules:

- **Agents.** `model`/`policy` map to `AgentSpec.Model`/`Policy`. `grants` become
  `AgentSpec.Tools` — each `tool.<name>.<operation>` grant reconstructed as a `uses` string,
  an autonomous capability bound, not a call list. Positions align in `AgentSpec.ToolsPos`.
- **Calls.** A dotted callee (`github.get_pr`) is a tool step (`uses: tool.github.get_pr`);
  a single identifier is a `workflow:` step when it names a workflow declared in the file
  (or listed in `Options.Workflows` for workflows in other files) and an `agent:` step
  otherwise. A name declared as both an agent and a workflow is a diagnostic, never a silent
  `agent:`. Named arguments become `with:` keys; positional arguments become placeholder
  `arg0`, `arg1`, … keys — **lowering itself has no symbol table**, so it cannot know a
  callee's real parameter names. For a `workflow:` step, the checker (#198, below) resolves
  those placeholders against the callee's declared, ordered parameters and **rewrites**
  `Program.Graph`'s `with:` keys to the real parameter names as a second pass over the
  already-lowered graph — a `.agent` program that type-checks clean also produces an
  executable graph, not one whose callee cannot read its own arguments. For an `agent:` step
  there is no such parameter list to rewrite against — an agent's `input` is one type, not
  named fields — so a multi-argument (or single-named) agent call's placeholder keys stay a
  real, open gap: #198 reports every such call loudly (a warning, or an error for zero
  arguments against a known input type) rather than silently guessing a field mapping, but
  does not resolve it.
- **References.** A workflow parameter field lowers to `${input.…}`; a binding lowers to
  `${steps.<id>.output.…}`. `return <expr>` lowers to `output.value.value`. A **bare**
  reference to a single-parameter workflow's input (the whole input object, e.g.
  `return input`) is a diagnostic: the interpolation language has no token for the entire
  input, only `input.<field>` (see the whole-input limitation below).
- **Nested calls** SSA-flatten: a call passed as an argument is hoisted into its own step
  (id `<parent>_arg<i>`) referenced by `${steps.<temp>.output}`.
- **Sequencing and `parallel`.** Statements chain through `needs` (issue #192): each
  statement waits on the previous frontier; a `parallel` block's branches share the
  pre-block frontier and fan in — their union is the next frontier.

### Identity is structural, never source location

Generated step ids derive from the program's structure — the enclosing workflow, the
binding name, and the AST child path for temporaries — never from `Pos`. `Pos` is diagnostic
metadata only ([ADR 003](adr/003-yaml-as-compilation-output.md)). Reformatting a program or
inserting unrelated lines above a binding therefore produces a **byte-identical** resource
graph, so `plan` shows no spurious diff. Positions are still carried — on the IR nodes
themselves (`WorkflowStep.*Pos`, `AgentSpec.ToolsPos`, #187) and in an auxiliary
`lower.SourceMap` keyed by structural identity — so a validation, policy, or effect
diagnostic on lowered IR underlines the `.agent` call site, not a synthesized name.

### Known limitation (whole-input reference)

The interpolation language addresses a workflow's input only as `input.<field>`
(`engine.resolvePath` requires at least two path segments), so there is no token for the
*entire* input object — unlike a step, whose whole output is `${steps.<id>.output}`. A bare
reference to a single-parameter workflow's input therefore is a diagnostic (lowering still
emits a best-effort `${input}` into a Result that, carrying a diagnostic, is not a valid
projection — callers check diagnostics before use). Supporting it needs two things that are
out of scope for the resource projection:
a whole-input interpolation token in the engine/validator, and — for passing a whole input to
a subworkflow — a callee input-document mapping rather than a one-key `with:` map (#194/#198).

### Known limitation (Epic F)

The ADR 002 `Reviewer` grants two operations on one tool
(`tool.github.read_pr`, `tool.github.read_comments`). `AgentSpec.Tools` as consumed by the
#160 agent loop currently advertises one operation per tool, so the lowered fixture is valid
on every axis lowering owns (steps, `needs`, references) but does not yet pass full
agent-spec validation. Lifting the one-operation-per-tool limit is tracked for Epic F
(#188/#204); lowering already preserves every grant.

## Type and effect checking (#198)

[`internal/lang/check`](../internal/lang/check) implements the ADR 002 §5 "checked
program" — the pass between the typed AST and the two sibling projections (the resource
projection above, and the execution lowering of [#199](#control-flow-and-the-execution-ir-199)):

```go
prog, diags := check.Check(f, check.Options{
    Project:   yamlSiblingGraph, // already-loaded YAML resources this .agent file references
    Files:     otherAgentFiles,  // other .agent ASTs in the same compilation unit
    SchemaDir: dir,              // base dir for the TypeRef -> schema convention below
})
```

`Check` always returns a non-nil `*check.Program`; `diags.HasErrors()` is the authority on
pass/fail (a `check.Program` from a failing `Check` still holds whatever it managed to
resolve, for tools that want partial results).

### The checked `effects` clause

`Check` computes the workflow's actual reachable effect set by **lowering every file in
the compilation unit (`f` plus `Options.Files`) through the existing `lower.LowerFile`,
merging all of them into one graph, and calling the existing, unmodified
`effects.Compute`** (#189) on the result — the same function the YAML ingress path already
uses. There is exactly one effect-bound algorithm in the codebase either way, which is what
makes the frontend and YAML paths agree on a bound by construction rather than by two
independently-tested implementations
(`TestDifferential_AgentAndYAMLProduceIdenticalEffectBounds` in `internal/lang/check` proves
this for a paired `.agent`/YAML fixture). Lowering the **whole** unit, not just `f`, matters
as soon as a callee lives in another `.agent` file: a callee classified but never lowered
would leave `effects.Compute` walking a resource that is not actually in the graph — a
workflow callee would then silently contribute no effects, and an agent callee would report
`Unknown` instead of its real grants.

The declared `effects { }` clause is then checked against that computed bound:

- **A computed effect the clause does not cover is an error.** This is the differentiating
  claim from ADR 002: the check covers autonomous tool selection exactly as `effects.Compute`
  does, so granting an agent a `destructive` tool fails compilation for every workflow that
  can transitively reach it. The diagnostic reuses `effects.FormatWitness` — the same witness
  rendering `internal/effects` uses for a policy violation (#190) — so the message shape,
  including the `AUTONOMOUS` tag on an agent-selection edge, is identical:

  ```text
  workflow "PRReview" may perform effect `destructive`, which its effects clause does not declare

    reachable via:
      Workflow/PRReview
        → step result  (Agent/Reviewer, AUTONOMOUS)
          → tool.github.merge_pr  [destructive]
  ```

- **A declared effect the body cannot reach is a warning, not an error.** The clause is an
  asserted upper bound; an over-broad one is not by itself a defect. This needed a
  `Severity` on `lang.Diagnostic` (`SeverityError` — the zero value, so every pre-existing
  diagnostic is unaffected — or `SeverityWarning`; `Diagnostics.HasErrors()` distinguishes
  them).
- **A reachable operation with no declared tool effects is always a violation**, matching
  `effects.Check`'s fail-closed stance — no clause can cover an unknown effect.
- A workflow with no `effects` clause is unchecked by this pass. This is a deliberate,
  independent product decision for this checker — **not** an analogue of `effects.Check`'s
  YAML behavior: that function is fail-closed (a workflow whose Policy carries no permit
  list permits *nothing* once any tool in the graph declares operation effects), which is
  the opposite of "unaffected." Whether an `.agent` workflow should be required to declare
  an effects clause at all is a separate lint decision.

### Type checking (agent invocation args, value flow)

`Check` also resolves `TypeRef` names (`ReviewRequest`, `PullRequest`, `Review`, …) and
checks call arguments and value flow between bindings against them, reusing the same
`schema.Document` / `schema.TypeSet.Compatible` primitives `internal/spec/wiring.go` uses
for YAML step wiring (#193) — but walking `CallExpr.Args` / `RefExpr.Parts` directly, so a
mismatch reports the `.agent` call-site position rather than a synthesized step name.
`TypeSet.Compatible` compares **JSON Schema type categories** (object, string, integer, …),
not schema identity — passing a `Review` where a differently-named-but-also-`object`
`ReviewRequest` is declared is not itself an error, the same coarseness #193 already has in
the YAML path. Nominal/structural schema equality is a separate, larger piece of work, not
part of this pass.

**A `TypeRef` name resolves to `<SchemaDir>/schemas/<Name>.json`** (`SchemaDir` defaults to
the directory of the `.agent` file being checked). This is a new naming convention
introduced by this package — no earlier ADR or grammar text specifies how a type name
becomes a schema, and it is the design decision most likely to need revisiting. A name with
**no matching file** checks as **untyped**, consistent with #193's gradual typing — a
missing schema is always compatible, never an error. A file that **exists but fails to
compile** is a different, louder case: `schema.LoadDocument` distinguishes a missing file
(`FileError`) from a broken one (`CompileError`), and only the former is gradual — a
present-but-invalid schema is reported as an error, since the author named a real file and
it did not parse.

What is checked:

- An agent invocation's **single positional argument** against the callee's declared
  `input` type — the one unambiguous shape, since an agent's `input` is one type, not a
  named parameter list. Every OTHER call shape against a known input type is a diagnostic,
  not a smaller version of the same problem to skip past quietly: **zero arguments** is an
  **error** (a declared input was never supplied); a **single named argument**
  (`A(input: x)`) and **more than one argument** — the ADR 002 normative surface's own
  `Synthesizer(security, quality, tests)` shape — have no defined field-order binding yet
  (lowering placeholder-keys those arguments `arg0`, `arg1`, … with no declared meaning for
  the receiving agent to bind against), so both emit an explicit **warning** naming the
  unchecked call rather than silently passing with no signal that nothing was verified.
- A workflow invocation's arguments against the callee's declared, **ordered** parameters,
  including call-site arity: a named argument naming no declared parameter, a positional
  argument past the last declared parameter, and a declared parameter left unbound by
  anything are all **errors**. Named and positional arguments may be mixed at one call
  site: a positional argument binds to the next declared parameter slot **not already
  claimed by a named argument anywhere in the call** (not to its raw position), so a named
  argument earlier in the call correctly "uses up" its slot instead of leaving a later
  positional argument double-checked against it. A `workflow:` step's lowered `with:` keys
  are rewritten to match this same binding (see the lowering section above) — the type
  checker and the resource projection agree on which argument fills which parameter.
- **Value flow through bindings**: `result = Synthesizer(...)` binds `result` to
  `Synthesizer`'s declared output type; a later `result.summary` walks that type through
  `schema.Document.Lookup`, and a field the schema declares forbidden
  (`additionalProperties: false`) is a positioned error.
- A `return <expr>` against the enclosing workflow's declared result type.
- A dotted (tool) callee's arguments are checked for their own internal well-formedness
  (nested calls, member access) but not against a declared parameter type — there is no
  `.agent`-visible tool schema.
- Only agents and workflows declared in the same compilation unit (`f` plus `Options.Files`)
  get resolved types; a callee that resolves only through `Options.Project` (a YAML-only
  sibling) is treated as untyped today — full YAML schema interop is a follow-up, not
  silently assumed.

## Control flow and the execution IR (#199)

Conditionals, loops, and dynamic fan-out are **computation**, not graph structure, so ADR
002 §4 forbids them from ever becoming a field on the resource-model `WorkflowStep`. They
live only in the **execution IR** — [`internal/execir`](../internal/execir) — the second of
the two sibling projections. `check.Check` populates `Program.Executables` with one
`execir.Program` per workflow; [`lower.LowerExec`](../internal/lang/lower/exec.go) produces
it by reading the AST directly (never the resource projection, which cannot represent
control flow).

### The surface

```text
workflow ReleaseAll(input: Batch)
    effects { github.read, github.write }
{
    if input.dry_run {
        report = github.summarize(input.repos)
    } else {
        parallel for repo in input.repos {
            github.deploy(repo, channel: "stable")
        }
        report = github.summarize(input.repos)
    }
    return report
}
```

`report` is bound in **both** arms, so it is definitely assigned after the `if` and `return
report` is well-formed. A binding made in only one arm is not in scope after the `if` (see the
scope rules below); return it inside that arm, or bind it in both.

- **`if` / `else` / `else if`** — a conditional; the condition is a boolean expression over
  already-bound values and literals (no calls; see the grammar note).
- **`for x in <collection>`** — a sequential loop over a runtime collection.
- **`parallel for x in <collection>`** — dynamic fan-out: one iteration per element, run
  with **bounded concurrency**. Each iteration has an isolated scope, so iterations never
  race and a body binding does not escape the loop.

### Scope and `return` — one model, in the checker and the interpreter

Sequential and parallel constructs scope bindings differently, and the type checker and the
interpreter implement the **same** rule so a program cannot type-check under one model and run
under another:

- **`if` is exclusive choice with a definite-assignment join.** The two arms never see each
  other's bindings (each is checked against the pre-`if` scope), and a binding is in scope
  after the `if` only if it is bound in **both** arms — `if c { x = A() } else { x = B() }`
  then a use of `x` is the intended idiom. A name bound in only one arm is not in scope
  afterward. When the two arms give a name different types the join is a union, represented as
  untyped/gradual (permissive) rather than whichever arm the checker walked last.
- **Sequential `for` may run zero times.** The loop variable is in scope inside the body, and a
  `return` inside returns from the workflow and halts the loop. But a name the loop **first**
  binds — the loop variable, or a binding introduced in the body — is **not** in scope after
  the loop, because an empty collection never binds it. A name that existed **before** the loop
  survives it (reassignment inside collapses its type to a union, never the last iteration's).

A reference to a name the scope model says is absent — a one-arm `if` binding used after the
`if`, a loop variable or body-local used after the loop, or any never-bound name — is a
**compile error** (`unresolved reference "…"`), reported by the type checker from the same
scope model the interpreter runs. This is what makes "the checker and interpreter share one
rule" a checked property rather than a claim: a program the scope model rejects does not
type-check, so it cannot reach the interpreter and fail there on an untaken path.
- **Parallel** (`parallel { … }`, `parallel for`): each branch/iteration runs in an **isolated**
  scope; only a `parallel {}` branch's own binding is published at the join, and a `parallel
  for` body's bindings do not escape. A `return` inside a parallel body is a **compile error**
  (there is no join target for a racing iteration's return value).

### Lowering targets (execution IR)

| Surface | `execir` node |
|---|---|
| `agent`/tool/`workflow` call | `InvokeAgent` / `InvokeTool` / `InvokeWorkflow` |
| `x = y` (alias) | `Let` |
| `parallel { … }` | `Fork` (branches run concurrently, join at the block's end) |
| `if … else …` | `Branch` |
| `for` / `parallel for` | `Loop` (`Parallel` set for fan-out) |
| `return e` | `Return` |

Nested-call arguments are hoisted into their own preceding `Invoke` bound to a fresh
temporary, so evaluation order matches source order. References use the **source binding
namespace** (parameter names, assignment targets, loop variables), not resource-model
`${steps.x}` tokens — the execution IR is independent of how the resource projection renders
interpolation.

### Effect soundness is the union over branches

The effect bound is **not** recomputed for control flow. `LowerFile` flattens every
conditional arm and loop body into resource steps (a sound over-approximation), and the
existing [`internal/effects`](../internal/effects) walk over those steps therefore yields the
**union over all reachable branches**. A branch that reaches an operation the workflow's
`effects { }` clause does not declare fails compilation exactly as a straight-line reach
would — a conditional cannot smuggle an unpermitted effect past the clause (ADR 002 §5).

### Bounded termination

Every loop is bounded, and the bound is decidable from the source text alone (ADR 002 §6):

- `for x in coll { … }` is bounded by the collection length.
- `while <cond> limit N { … }` is bounded by the **mandatory** `limit N`, a positive integer
  literal fixed in source. There is **no** unbounded effectful `while` — a naked
  `while cond { … }` would let a nondeterministic agent drive an unbounded number of tool and
  model invocations, which is exactly what the platform bounds.

The interpreter enforces both, independently of the compiler: for a `while` the effective
ceiling is `min(N, limits.maxLoopIterations)` (`spec.DefaultMaxLoopIterations` = 1000;
overridable per project/workflow), and the body runs **at most** that many times **even if the
condition never becomes false** — an adversarial or malformed carried state cannot buy a
`limit+1`th iteration. The per-loop `limit` is the semantics of the construct; the global cap is
only an additional backstop. The iteration bound is a separate axis from the effect bound: the
effect set is the union over the loop body's reachable steps regardless of `N` (no quantitative
effect algebra).

**Loop-carried state.** A `while` body runs on the enclosing scope (like a sequential `for`), so
a binding that existed **before** the loop may be rebound inside and carries forward across
iterations and out of the loop (last write wins). A binding first introduced **inside** the loop
is loop-local — it is recreated each iteration and is **not** visible after the loop, because the
loop may run zero times. Workflow parameters stay immutable, so carry state through a local:

```agent
state = input
while !state.approved limit 3 {
    implementation = Implementer(state)   // loop-local
    state = Reviewer(implementation)      // preexisting: carries forward
}
return state
```

Durable replay stays sound across iterations: each iteration folds its index into the leaf's
call identity, so an effectful leaf in iteration *i* memoizes under a stable, iteration-specific
key and is never reissued on resume, and each iteration's condition is recorded so a resume
reproduces the same iteration history (a divergent condition is a loud error, not a silently
different run).

### `plan` under control flow — the execution IR is part of workflow identity

`plan` diffs the **resource projection**, but a workflow's `spec_hash` also commits to the exact
executable IR whenever one exists (#260):
[`plan.WorkflowSpecHashWithExec`](../internal/plan/workflow_hash.go) folds
`execir.Program.Digest` into the hash, so a **lowering-only change** (e.g. swapping an `if`'s two
arms, or a compiler change that alters the `Program` without touching the resource projection)
is a visible plan change. `project.LoadProjectWithExecutables` builds the `Program` for every
workflow (`check.Check`'s checked `.agent` programs; `LowerWorkflowResource` for YAML) and
`ComputePlan` folds each workflow's program digest. The invariant is not tied to control flow:
identity is the normalized resource projection **and** the executable IR whenever it exists.

### Convergence status

ADR 002 §5's convergence goal — both ingress paths sharing one interpreter — is reached for the
control-flow surface: YAML lowers to `execir` (`LowerWorkflowResource`, #256), the engine
executes `execir` at parity with the DAG (#257), durably resumes it including HITL, concurrent
per-branch suspend, and nested subworkflows (#258/#270), pins the program into the deployment
snapshot (#260), and runs `.agent` control flow end-to-end (#259). The compiled program is
persisted as an `execution_ir` deployment artifact and hydrated on resume, so an in-flight run is
never re-lowered underneath it (ADR 001).

The one remaining step is retiring the redundant `WorkflowStep` DAG runtime so **every** workflow
(not only control-flow ones) runs on `execir` — tracked in **#278**. Until then, straight-line /
`needs` / `parallel { }` workflows still execute on the DAG.

## Diagnostics

`Parse` always returns a non-nil `*File` (possibly partial) plus a
`lang.Diagnostics` slice sorted by position. Each `lang.Diagnostic` carries a `Pos`, a
message, and a `Severity` (`SeverityError`, the zero value, or `SeverityWarning` — added by
#198 for the over-broad-effects-clause case above) and formats as
`file:line:col: message` (a warning is prefixed `warning:`). `Diagnostics.HasErrors()`
reports whether at least one entry is fatal, and `Diagnostics.AsError()` converts to a plain
`error` — `nil` for a warning-only result, non-nil otherwise. **Do not** treat a bare
`error(diags) != nil` check, `len(diags) != 0`, or `fmt.Errorf("%w", diags)` as "this
failed": `Diagnostics` is a non-nil slice type once populated, so all three report failure
for a warning-only result regardless of what `Error()`'s string says. `AsError` (or
`HasErrors` directly) is the only safe conversion.
