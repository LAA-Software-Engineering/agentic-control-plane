# Implementation plan — #198: Type and effect checking, including the checked `effects` clause

Status: in progress. Depends on #197 (lowering, merged), #189 (effect bounds, merged), #190
(policy enforcement, merged), #193 (typed step outputs, merged) — all landed.

**Delivered so far** (slices 1–3 below, combined into one PR rather than three): the
`internal/lang/check` package as specified — symbol/type resolution, the checked `effects`
clause (design decision 6), and invocation-argument/value-flow type checking (design
decisions 4–5) — plus the differential test and the `docs/LANGUAGE.md` section (slice 4).
Remaining: wiring `.agent` ingress into `agentctl`/`project.LoadProject` (explicitly out of
scope per design decision 8) and full YAML-schema interop for cross-ingress type checking
(noted as a follow-up in `internal/lang/check/types.go`'s `typeUniverse` doc comment).

## Goal

Implement the "checked program" from [ADR 002 §5](../adr/002-language-frontend-and-ir-expressiveness.md#5-resource-ir-and-execution-ir-are-sibling-projections-not-a-pipeline):

```text
.agent  →  typed AST  →  checked program  →  { resource projection, execution lowering }
                         (resolved refs · types · effect bound)
```

The resource projection (#197) already exists and is a **sibling** of this work, not its
input — `internal/lang/lower/doc.go` states plainly that "until the checker (#198) lands,
the 'checked program' is the raw AST." This issue builds the missing middle box: a new
`internal/lang/check` package that resolves every reference in a `.agent` file, resolves
its type names, computes the workflow's *actual* reachable effect set, and checks it
against the declared `effects { }` clause — which becomes a genuinely **checked**
declaration rather than documentation, per the ADR 002 normative example.

Execution lowering (#199) is not started and is out of scope here; per ADR 002 §5 it must
read the checked program independently of the resource projection, so this package's
output (`check.Program`) is designed to be that shared input, not something #197- or
#199-specific.

## Design decisions (and why)

### 1. Reuse `lower` + `effects.Compute` for the bound; do not write a second effect walker

Issue #198 requires "the frontend check and the YAML-path check must agree exactly... test
that agreement directly." Two independent implementations of effect-graph traversal can
only be *tested* into agreement, and drift silently the next time either one changes. This
plan instead makes agreement **structural**: the checker lowers the AST via the existing
`lower.LowerFile` (unmodified) into a `*spec.ProjectGraph`, merges it with any sibling YAML
resources in the project (`project.MergeLowered`), and calls the existing, unmodified
[`effects.Compute`](../../internal/effects/compute.go) on the result — the same function
the YAML path already uses. There is exactly one effect-bound algorithm in the codebase
either way.

The differential test (Test requirements) therefore exists to prove **lowering** preserves
effect-relevant semantics (a `.agent` program and its hand-written YAML equivalent lower to
graphs with identical bounds), not to reconcile two graph walkers. This also means the
checker inherits every soundness property #189/#190 already have (cycle safety, fail-closed
unknown effects, static/autonomous tagging) for free.

### 2. Package shape and API

```go
package check // internal/lang/check

type Options struct {
    // Project supplies already-loaded sibling YAML resources (agents, tools, policies)
    // so a .agent file can reference them — e.g. `policy guarded-writes` naming a YAML
    // Policy resource. Nil is treated as an empty graph.
    Project *spec.ProjectGraph

    // Files supplies other .agent ASTs in the same compilation unit, so cross-file
    // agent/workflow references resolve. This replaces lower.Options.Workflows, which
    // internal/lang/lower/lower.go already documents as a #198 stopgap.
    Files []*lang.File
}

// Program is the checked program: resolved references, resolved types, and the
// computed effect bound for every workflow and agent, ready for either projection.
type Program struct {
    File    *lang.File
    Symbols *SymbolTable
    Types   map[TypeRefKey]*schema.Document // resolved TypeRef -> loaded schema, or nil (gradual typing)
    Graph   *spec.ProjectGraph              // lowered + merged graph the bound was computed on
    Bounds  effects.GraphBounds             // effects.Compute(Graph), reused verbatim
}

// Check resolves f against opts, type-checks it, computes and checks the effects
// clause on every workflow, and returns the checked program plus diagnostics.
// A non-nil Program is still returned on error so callers (e.g. tests, future
// execution lowering) can inspect partial results; diagnostics is the source of truth
// for pass/fail.
func Check(f *lang.File, opts Options) (*Program, lang.Diagnostics)
```

`internal/lang/lower.Options.Workflows map[string]bool` becomes redundant once `check`
ships its project-wide symbol table; `lower.LowerFile` keeps working standalone (it is
still the entry point the checker itself calls), but the checker computes and passes a
complete `Options.Workflows` rather than relying on the caller to enumerate it by hand.

### 3. Symbol resolution

`SymbolTable` resolves, across `opts.Files` plus `f` itself:
- every `AgentDecl.Name` / `WorkflowDecl.Name` to its declaring file+node (duplicate names
  across files, or an agent/workflow name collision, is a diagnostic — lowering already
  diagnoses same-file collisions; this extends it project-wide),
- every workflow-body callee (`CallExpr.Callee`, a `RefExpr`) to one of: a workflow param
  (`RefExpr.Parts[0]` matches a `Param.Name`), a binding (an `AssignStmt.Target` earlier in
  the same workflow or an enclosing `parallel` branch), a same-project `AgentDecl`, a
  same-project `WorkflowDecl`, a dotted tool operation (`github.get_pr`), or none of the
  above — an unresolved-reference diagnostic with position,
- `AgentDecl.Policy` against `opts.Project.Policies`,
- `Grant.ToolName()` against `opts.Project.Tools` and the tool's declared operations
  (a grant naming an undeclared tool or operation is a checker error — ADR 002's "grants
  must never be written in terms of effects" invariant starts here: a grant is only valid
  if it names a real concrete operation).

This is what makes `Program.Graph` buildable: `check.Check` calls `lower.LowerFile` with
`lower.Options{Workflows: table.WorkflowNames()}` once resolution succeeds, then
`project.MergeLowered` folds the result onto `opts.Project` for `effects.Compute`.

### 4. Type resolution — new convention, flagged for review

`TypeRef.Name` (`ReviewRequest`, `Review`, `PullRequest`, …) is, today, just free text —
neither #196's grammar nor ADR 002 defines how a type name becomes a schema. This plan
proposes the smallest convention consistent with existing YAML practice (every shipped
example already keys schemas as `./schemas/<slug>.json`):

> A `TypeRef` named `Foo` resolves to `<dir>/schemas/Foo.json` relative to the `.agent`
> file's directory, loaded with the existing `schema.LoadDocument`. A missing file is
> **not** an error — consistent with #193's gradual typing ("absent schemas remain
> permitted"), an unresolved type name checks as untyped and only affects diagnostics that
> require a schema (member-access type checks on it are skipped, not failed).

**This is a genuinely new naming rule with no prior art in the codebase or ADR text — it
is the one design decision in this plan most likely to need reviewer input before
implementation starts.** An alternative considered: require a `type Foo = "./schemas/foo.json"`
alias declaration in the grammar. Rejected for this plan because ADR 002 is deliberately
hostile to surface-syntax growth ("No anonymous functions... every callable is a declared
resource") and a filename convention costs no grammar change, matching how Go resolves
package names by directory convention rather than an explicit mapping table.

Resolved types back:
- agent `input`/`output` checking against the same `schema.Document` shape already used by
  `AgentIO.Resolved`,
- workflow `Result` type against the lowered `return` expression's inferred type,
- param types against call-site argument types.

### 5. Invocation-argument and value-flow type checking

Mirrors [`internal/spec/wiring.go`](../../internal/spec/wiring.go)'s `validateStepWiring`
(#193) — same `schema.Document.Lookup` / `TypeSet.Compatible` primitives — but walks AST
nodes directly instead of parsing interpolation strings, so a mismatch reports the
`.agent` call-site position with no synthesized-name indirection:

- For `CallExpr.Args`, resolve each `Arg.Value` to a `schema.TypeSet` (a `RefExpr` chain
  walks `Types[...].Lookup(parts)`; a nested `CallExpr` result uses the callee's declared
  output type) and check it against the callee's declared parameter/input type at that
  position with `schema.Compatible`.
- For `AssignStmt`, the binding's type is the value expression's resolved type (agent
  call → agent's `Output` type; tool call → unresolved/untyped, since tools have no
  `.agent`-visible schema today; nested workflow call → callee's `Result` type).
- A later reference to that binding (`result.summary`) walks the remembered type through
  `schema.Document.Lookup` the same way `wiring.go` does for `${steps.x.output.summary}`.
- `return <expr>` is checked against the enclosing `WorkflowDecl.Result` type.

Untyped values (missing schema, either side) are always compatible — this is the same
gradual-typing rule #193 already established and must not regress it.

### 6. The checked `effects` clause

For each `WorkflowDecl` with a non-nil `Effects` clause:

1. Look up `Program.Bounds.Workflows[name]` (already computed by the reused
   `effects.Compute`).
2. **Computed ⊄ declared → error.** For every `effects.Effect` in the bound whose `Ident`
   is not covered by any declared `EffectRef.Name` (`spec.EffectCovers(declared, ident)` —
   reused verbatim, so `github` in the clause covers a computed `github.read`), emit a
   diagnostic. The message and witness rendering **reuse** #190's formatter: this plan
   exports `effects.FormatWitness(hops []effects.Hop, ident string, unknown bool, uses
   string) string` (today `formatWitness`, unexported, in `internal/effects/enforce.go`) —
   a one-line visibility change, not a reimplementation — so the checker's diagnostic body
   is byte-identical in shape to the existing policy-violation message, `AUTONOMOUS` tag
   included. An `Unknown` computed effect (operation with no declared tool effects) is
   always a violation, matching `effects.Check`'s existing fail-closed stance.
3. **Declared ⊄ computed → warning, not error.** For every declared `EffectRef` not
   covered by any computed effect, emit a **warning** diagnostic ("declares `X` but the
   body cannot reach it"). This is structurally the same shape as `Bound.Unreachable`, but
   scoped to declared-clause idents rather than declared-tool-operation idents, so it is a
   small adapter over existing data (`Program.Bounds`), not a new traversal.
4. A workflow with **no** `effects` clause is unchecked by this pass (existing YAML
   behavior — a workflow with no `Policy.spec.effects` is unaffected by #190 either);
   whether an `.agent` workflow should be *required* to declare effects is a policy-lint
   decision, not this issue's.

### 7. Diagnostics need a severity

`lang.Diagnostic` (`internal/lang/diagnostic.go`) is currently `{Pos, Msg}` with no
severity — every existing diagnostic (lexer/parser/lowering) is an error. Requirement 6.3
above needs a warning that must not fail compilation. This plan adds:

```go
type Severity int

const (
    SeverityError Severity = iota // zero value — every existing diagnostic stays an error
    SeverityWarning
)

type Diagnostic struct {
    Pos      Pos
    Msg      string
    Severity Severity
}
```

Zero-value default preserves every existing call site (`diagf`, parser/lexer/lowering
diagnostics) as `SeverityError` with no changes required there. `Diagnostics.Sorted()`,
`.Error()` are unaffected; a new `Diagnostics.HasErrors() bool` helper lets callers
(CLI, tests) treat warnings as non-fatal.

### 8. Scope boundary: no `agentctl`/project-loader wiring in this issue

Per the research into #197's follow-ups, `.agent` files are not yet ingested by
`project.LoadProject` or any `agentctl` command — that wiring is unscheduled Epic H work,
not named in #198's acceptance criteria. This plan scopes #198 to the `internal/lang/check`
library and its tests (plus the differential test harness, which drives `lower`+`check`
directly rather than through a CLI). Wiring `.agent` into `agentctl validate`/`plan` is a
natural follow-up issue, called out explicitly so it isn't assumed silently in scope here.

## Known gaps inherited from #197 (not this issue's job to fix)

- **Multi-operation-per-tool grants** (#188/#204): `effects.Compute`'s autonomous walk
  goes through `spec.ResolveAgentAdvertisedTools`, which is one-operation-per-tool. The
  ADR 002 fixture's `Reviewer` (granted two `github` operations) inherits the same
  documented gap #197 pinned with `TestLower_MultiOperationGrantIsKnownGap`. The checker
  computes a bound over whatever `AgentSpec.Tools` lowering actually produced — it does not
  paper over this gap, and the differential test must pin it as a known, not silently
  passing, discrepancy for the ADR 002 fixture specifically.
- **Whole-input token** (`return input` on a single-param workflow): lowering already
  diagnoses this. The checker's value-flow typing treats the diagnosed lowering output as
  untyped for that binding rather than crashing.

## Files

- `internal/lang/check/doc.go` — package contract: what "checked program" means here, the
  explicit non-goal of reimplementing effect computation, pointer to ADR 002 §5.
- `internal/lang/check/check.go` — `Check`, `Program`, `Options`.
- `internal/lang/check/symbols.go` — `SymbolTable`, reference resolution.
- `internal/lang/check/types.go` — `TypeRef` → `schema.Document` resolution, the
  invocation-argument / value-flow checker.
- `internal/lang/check/effects.go` — the checked-clause comparison (design decision 6).
- `internal/lang/check/*_test.go` + `internal/lang/check/testdata/` — table tests per
  category below.
- `internal/lang/diagnostic.go` — add `Severity` (design decision 7).
- `internal/effects/enforce.go` — export `formatWitness` → `FormatWitness` (no behavior
  change; existing callers updated).
- `internal/lang/lower/lower.go` — no functional change expected; `Options.Workflows`
  doc comment updated to point at `check.SymbolTable` as its replacement once this ships.
- A differential-test fixture pair: reuse `internal/lang/lower/testdata/adr002.agent`
  alongside a new hand-written YAML equivalent (or an existing `examples/pr-review-*`
  project shaped the same way) under `internal/lang/check/testdata/differential/`.
- `docs/LANGUAGE.md` — new "Type and effect checking" section (the file's own header
  currently flags this section as missing).
- `CHANGELOG.md` — Unreleased entry (added when implementation lands, not in this
  plan-only PR).

## Test matrix (issue "Test requirements")

- **Effects-clause table tests**: declared clause is an exact cover (pass); declared is a
  strict superset (warning, not error, on the unreached ident); computed exceeds declared
  via a **static** `uses:` step (error, `static` tag in witness); computed exceeds declared
  via an **autonomous** agent grant (error, `AUTONOMOUS` tag in witness); an `Unknown`
  reachable operation (tool with no declared effects) always violates regardless of the
  clause; no `effects` clause present (no check performed).
- **Type-checking table tests**: matching agent invocation arg type (pass); mismatched
  scalar type at a call argument (error, position at the argument); value flow through two
  bindings (`pr = github.get_pr(...); x = Reviewer(pr)`) where `Reviewer`'s declared input
  type disagrees with `pr`'s inferred type (error); missing schema file for a `TypeRef`
  (gradual — no error, diagnostic-free); member access past a schema's declared shape
  (`result.nonexistent_field`, error mirroring `wiring.go`'s "not declared in schema").
- **Differential test**: lower the ADR 002 `.agent` fixture and an equivalent hand-authored
  YAML project, run `effects.Compute` on both resulting graphs, assert the two
  `effects.GraphBounds` are equal after normalizing away step-id naming differences (the
  two ingress paths do not need identical step IDs, only identical effect idents,
  reachability tags, and witness *shape*). Document the multi-operation-grant known-gap
  exclusion explicitly rather than silently masking it.
- **Golden diagnostics**: golden `.txt` fixtures for one effects-clause violation message
  and one type-mismatch message, following the existing `GO_UPDATE_GOLDEN=1` convention
  used by `internal/lang/lower/lower_test.go`.

## Suggested sequencing

This issue is large enough that landing it as one PR risks an unreviewable diff. Proposed
incremental slices, each independently mergeable and each leaving `main` green:

1. **Scaffolding**: `internal/lang/check` package skeleton, `SymbolTable`, project-wide
   reference resolution (no type or effect checking yet — just "does every name resolve").
   `lang.Diagnostic.Severity` added here since it's a small, isolated change.
2. **Effects-clause checking** (design decision 6) — the ADR-002-differentiating feature
   and the one most directly named by the issue title. Ships the `effects.FormatWitness`
   export and the differential test harness, since both are needed to validate this slice
   on their own.
3. **Type checking** (design decisions 4–5) — agent invocation args and value flow. Ships
   the type-name-to-schema convention as its own reviewable decision, separate from the
   effects work so a disagreement on the convention doesn't block the effects slice.
4. **Docs**: `docs/LANGUAGE.md` effect-system section, `CHANGELOG.md`.

This mirrors how Epic F itself shipped (#188 → #189 → #190 → #191 as separate, reviewable
PRs building on each other) rather than one large one.

## Acceptance criteria mapping

| Issue acceptance criterion | Where satisfied |
|---|---|
| Body exceeding `effects` clause fails to compile, witness + `AUTONOMOUS` tags | Design decision 6, step 2; slice 2 |
| Autonomous tool selection included in computed bound | Free — reused `effects.Compute` already does this (#189) |
| Adding a destructive effect to a tool breaks every reachable workflow | Free — same reuse; no new logic needed beyond decision 6 |
| Over-broad clause warns | Design decision 6, step 3; requires diagnostic severity (decision 7) |
| Type mismatches in agent invocation / value flow caught with positions | Design decisions 4–5; slice 3 |
| Frontend and YAML paths produce identical bounds | Design decision 1 (structural reuse) + differential test |

## Out of scope (follow-ups)

Execution lowering (#199, unstarted); wiring `.agent` ingress into `agentctl`/
`project.LoadProject` (Epic H, not named in #198's acceptance criteria — see design
decision 8); multi-operation-per-tool agent advertising (#188/#204, inherited gap); a
`type Foo = "..."` alias declaration, should the filename convention in design decision 4
prove insufficient in review (would require a #196 grammar amendment, out of this issue's
authority).
