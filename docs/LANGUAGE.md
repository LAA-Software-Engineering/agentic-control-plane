# The `.agent` language — grammar reference

`.agent` is the surface syntax for authoring agents and workflows, fixed by
[ADR 002](adr/002-language-frontend-and-ir-expressiveness.md). This document is the
grammar reference for the **frontend as shipped in issue #196: lexing, parsing, and
the typed AST only.** There is no lowering to the resource model yet (#197), no type
or effect checking (#198), and no conditionals, loops, or dynamic fan-out (#199).

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

## Diagnostics

`Parse` always returns a non-nil `*File` (possibly partial) plus a
`lang.Diagnostics` slice sorted by position. Each `lang.Diagnostic` carries a `Pos` and
a message and formats as `file:line:col: message`.
