# The `.agent` language — grammar reference

`.agent` is the surface syntax for authoring agents and workflows, fixed by
[ADR 002](adr/002-language-frontend-and-ir-expressiveness.md). This document is the
grammar reference for the **frontend as shipped in issue #196: lexing, parsing, and
the typed AST only** — plus the **resource-model lowering added in #197** (see
[Lowering to the resource model](#lowering-to-the-resource-model-197) below). There is no
type or effect checking yet (#198), no execution lowering / `Branch`/`Loop` (#199), and no
conditionals, loops, or dynamic fan-out.

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

Statement   = Assign | Parallel | Return | ExprStmt ;
Assign      = Ident "=" Expr ;
Parallel    = "parallel" "{" { Assign } "}" ;
Return      = "return" Expr ;
ExprStmt    = Expr ;                            (* a call for its effect *)

Expr        = Ref [ "(" [ Args ] ")" ] ;        (* Ref, or a call on Ref *)
Ref         = Ident { "." Ident } ;             (* pr, input.repo, github.get_pr *)
Args        = Arg { "," Arg } ;
Arg         = [ Ident ":" ] Expr ;              (* named or positional *)
```

Notes:

- The `effects` clause accepts commas between effects or none, so it reads the same
  whether comma- or newline-separated. Grants are newline-separated (a comma between
  grants is tolerated for symmetry).
- A `parallel` block admits only assignments: each branch binds a name for fan-in.
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
  `arg0`, `arg1`, … keys pending parameter binding (#198).
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

## Diagnostics

`Parse` always returns a non-nil `*File` (possibly partial) plus a
`lang.Diagnostics` slice sorted by position. Each `lang.Diagnostic` carries a `Pos` and
a message and formats as `file:line:col: message`.
