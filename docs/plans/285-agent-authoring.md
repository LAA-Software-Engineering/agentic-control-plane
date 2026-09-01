# Implementation plan — #285: Author real agents in `.agent`

Status: **PLANNED**. Epic [#285]. Nine issues filed: #286 (J1), #287 (J2), #288 (J3), #289 (J4),
#290 (J5), #291 (J6), #292 (J7), #293 (J8), #294 (J9). Builds on the execir engine convergence
(#255 epic — control flow runs end-to-end via the pinned program, #259/#260) and assumes the
`WorkflowStep` DAG runtime is retired (#278). Depends on ADR 002 (language frontend and IR
expressiveness).

> **Framing.** This plan documents work that is filed but not yet built. It is a plan, not a grammar
> reference — the normative surface in [`docs/LANGUAGE.md`](../LANGUAGE.md) and the decisions in
> [ADR 002](../adr/002-language-frontend-and-ir-expressiveness.md) describe *shipped* behavior and
> are updated by the feature issues as each lands, not by this document.

## Goal

Extend `.agent` so it can fully author real agents, then ship a flagship example that demonstrates
Terfyn as a **bounded execution platform for nondeterministic multi-agent programs**. The statement
this epic must make concretely true:

> The model may behave nondeterministically, but Terfyn defines the maximum authority it can
> exercise, the maximum number of bounded control-flow iterations around it, makes authority changes
> reviewable before deployment, and durably resumes execution without duplicating completed side
> effects.

The target program (syntax may adjust to parser conventions; semantics fixed):

```agent
agent Implementer {
    model anthropic/claude-sonnet-4-20250514
    policy coding-agent
    instructions """
    You are the implementation agent.
    You receive a CodingState and, on later attempts, review feedback.
    Implement the requested change in the provided workspace.
    Do not claim tests passed unless you actually ran them successfully.
    """
    grants {
        tool.workspace.read_file
        tool.workspace.write_file
        tool.workspace.run_tests
    }
    input CodingState
    output CodingState
}

agent Reviewer {
    model openai/gpt-5
    policy reviewer
    instructions """
    You are the independent code reviewer.
    You may inspect files and run tests. You must not modify the workspace.
    Set approved to true only if the implementation is acceptable.
    """
    grants {
        tool.workspace.read_file
        tool.workspace.run_tests
    }
    input CodingState
    output CodingState
}

workflow ImplementAndReview(input: CodingState) -> CodingState
    effects { workspace.read, workspace.write, process.exec }
{
    state = input
    while !state.approved limit 3 {
        implementation = Implementer(state)
        state = Reviewer(implementation)
    }
    return state
}
```

## Where things stand (what `main` actually supports)

Read before implementing — these are verified against current `main`, not stale assumptions.

- **`instructions` has a clean destination.** `AgentSpec.Instructions string`
  ([internal/spec/kinds.go:79](../../internal/spec/kinds.go)) already exists and is runtime-consumed.
  `AgentDecl` ([internal/lang/ast.go:46](../../internal/lang/ast.go)) has no field for it and the
  lexer scans only single-quoted strings ([internal/lang/lexer.go:214](../../internal/lang/lexer.go)).
  Lowering only has to copy the AST string into `Instructions` — no new prompt abstraction, no new
  runtime semantics.
- **The type checker already implements loop-carried state.** `checkStmt`'s `*ForStmt` case
  ([internal/lang/check/types.go:292](../../internal/lang/check/types.go)) uses `loopJoin(pre, env)`:
  a name bound **before** the loop survives after it (collapsed to an untyped union, since a
  sequential loop may run zero times); a name **first bound inside** does not escape. That is exactly
  the rule bounded `while` needs. The interpreter matches it — a sequential `Loop` runs on the
  enclosing scope, last iteration wins ([internal/execir/interp.go:492](../../internal/execir/interp.go)).
- **execir call identity already folds loop-iteration indices.** `CallSite{Bind, Path, Loop}` and
  `CallKey` ([internal/execir/interp.go:230](../../internal/execir/interp.go)) key each leaf by static
  path **plus** the enclosing iteration vector; `controlKey` records each pure control decision (a
  `Branch` condition, a `Loop` collection length) per path+loop so replay asserts non-divergence and
  fails loudly. Bounded `while` is a minimal extension: append the iteration index to `CallSite.Loop`
  exactly as the sequential `Loop` does, and record the per-iteration condition through the same
  `checkControl`. Durable memo/resume (`RunState.Memo`/`Control`) then replays completed leaves
  without reissuing effects — the property #258/#275 already establish for `for`.
- **The runtime already caps iterations.** `Interp.maxIters()` bounds every loop by
  `spec.DefaultMaxLoopIterations`. The per-loop `limit N` becomes an **additional, source-explicit**
  ceiling: `effectiveMax = min(limit, global)`. Prefer catching `limit > global` at check time.
- **Effect analysis needs no change.** The effect bound is the union over reachable steps; `limit N`
  is an iteration count, **not** an effect multiplier. Keep the two concepts separate — no
  quantitative effect algebra (ADR 002; Part 10 of the epic).
- **One real capability gap.** The agent loop advertises **one operation per tool name**:
  `ResolveAgentAdvertisedTools` rejects a tool declared twice with different operations
  ([internal/spec/agent_tools.go:62](../../internal/spec/agent_tools.go)), and lowering documents this
  as a known gap ([internal/lang/lower/lower.go:188](../../internal/lang/lower/lower.go);
  `TestLower_MultiOperationGrantIsKnownGap`; LANGUAGE.md "Known limitation (Epic F)"). The flagship
  Implementer needs three operations on one `workspace` tool — resolved deliberately in J6, not
  papered over.
- **Typed agent I/O is resolved but not lowered.** The checker resolves `input`/`output` schemas via
  the `schemas/<Name>.json` convention (`agentTypeInfo`,
  [internal/lang/check/types.go:13](../../internal/lang/check/types.go)), but lowering records only
  source-map positions and leaves `AgentSpec.Input`/`Output` nil
  ([internal/lang/lower/lower.go:206](../../internal/lang/lower/lower.go)). The flagship's typed
  `CodingState` flow needs these wired through — J9.
- **Plan already has the authority-widening machinery.** `Authority.Autonomous == AuthorityWidened`,
  `RiskCategoryAuthorityWidening`, and `WitnessAutonomous` witnesses already exist
  ([internal/plan/effects.go:60](../../internal/plan/effects.go)). The Reviewer gaining
  `tool.workspace.write_file` is an autonomous authority widening the existing planner surfaces — the
  example exercises it; no new planner concept is required.

## `while` belongs on the execir side, never the DAG

ADR 002 §4 names `while` among the keywords that must never become a `WorkflowStep` field, and
[execir.go:150](../../internal/execir/execir.go) records "there is no unbounded (`while`) loop in the
surface." This work respects both: `while` is lowered to the execution IR and executed by the
interpreter. Nothing here touches `execution_dag.go`, adds a `WorkflowStep` field, or reintroduces a
DAG compatibility branch (#278 retired that runtime). The resource projection may conservatively
flatten the loop body for effect analysis, but it never executes the loop.

## The safety property, made concrete

```
                  max 3 iterations (explicit in source: `limit 3`)
                        |
                        v
                  Implementer  ── capability bound: read_file, write_file, run_tests
                      |
                      v
                   Reviewer    ── capability bound: read_file, run_tests   (NO write_file)
                  /        \
           approved          rejected
              |                 |
              v                 |
            return <------------+
```

Two independent bounds, kept separate:

- **Authority** — each agent's grants declare the concrete operations it may invoke. A Reviewer that
  attempts `workspace.write_file(...)` — hallucinating, prompt-injected, or mistaken — is denied
  because the operation is outside its declared capability, **not** because its prompt said "do not
  edit." The capability system wins even when the prompt does not.
- **Iterations** — `limit 3` bounds how many implement/review rounds may run, enforced by the
  interpreter regardless of what the (adversarial) carried state claims. Neither cost policy nor
  wall-clock timeout is the only protection against unbounded model/tool activity.

## Issues (dependency order)

### J1 — Triple-quoted multiline string literals (#286)
Lexer/AST/parser/printer for `"""…"""`, lexing to an ordinary string value with deterministic
indentation normalization; single-line `"…"` stays backward compatible; `terfyn fmt` round-trips.
Foundation for J2. No interpolation.

### J2 — `instructions` agent field (#287, depends on J1)
New `AgentDecl` field parsed as an agent field, duplicate → first-wins diagnostic, lowered verbatim
into `AgentSpec.Instructions`. Export/reload preserves it. No new runtime semantics.

### J3 — Bounded `while cond limit N` (#288)
Grammar (`While = "while" Cond "limit" Number Block`), AST `WhileStmt`, parser with a
positive-integer-literal `limit` (reject missing/zero/fractional/dynamic), checker reusing
`loopJoin` for loop-carried scope, printer round-trip. Workflow parameters stay immutable
(`input = …` invalid; carry `state = input`). No unbounded `while`, no `parallel while`.

### J4 — Lower `while` to execir + runtime bound (#289, depends on J3)
A `While` node (recommended over overloading `Loop`) folded into `Program.Digest`; sequential
execution on the enclosing scope with `extend(loop, idx)` per iteration; `effectiveMax = min(limit,
maxIters())` enforced by the interpreter; per-iteration condition recorded via `checkControl`.
Non-resumable parity; no DAG involvement.

### J5 — Durable `while` (#290, depends on J4) — the correctness core
Stable per-iteration `CallKey`; completed leaves replay from `RunState.Memo` on resume, never
reissued; condition replay through the control-record with **loud** divergence; HITL gate inside the
loop suspends/resumes at the correct iteration. Reuses #258/#270/#275 machinery.

### J6 — Multiple operations per Tool grant (#291)
Lift the one-operation-per-tool limit so a single autonomous grant exposes `read_file` +
`write_file` + `run_tests`, each a distinct advertised operation the model may call, each gated
independently. Preferred: advertise multiple operations of one Tool as distinct tool-defs; fall back
to multiple Tool resources only if that is not minimal. Retire/refresh
`TestLower_MultiOperationGrantIsKnownGap` and the LANGUAGE.md known-limitation note.

### J7 — Flagship example `examples/implement-review-loop` (#292, depends on J2, J5, J6, J9)
One `.agent` file (agents + workflow), supporting YAML for tools/policies/config, a real
`CodingState` JSON Schema, a README explaining *deterministic bounded control around nondeterministic
agents*, the capability-vs-prompt distinction, and a representative `terfyn plan` authority-widening
walkthrough. Runs exclusively through execir; the Reviewer's `write_file` attempt is denied at the
capability boundary.

### J8 — `while` invocation-count bounds in `plan` (#293, follow-up)
Surface a coarse per-agent invocation upper bound derived from enclosing `while`/`for` bounds.
Structural preservation of `limit` (J3/J4) is required; the `plan` surfacing is a follow-up and must
not build a symbolic execution engine.

### J9 — Lower agent `input`/`output` into `AgentSpec` schema fields (#294, blocks J7's typed flow)
Populate `AgentSpec.Input`/`Output.Schema` from the `input`/`output` type refs using the same
`schemas/<Name>.json` convention the checker already applies, so validate resolves the schema and the
runtime enforces structured agent I/O for `.agent`-authored agents.

## Top risks & open questions

1. **Multiline escape policy (J1).** Decide raw vs escape-processed inside `"""…"""` and document it
   in one place (lexer doc comment + LANGUAGE.md). Raw is the simplest sound default for prose
   prompts; if escapes are kept they must match the single-line set.
2. **`While` node vs overloading `Loop` (J4).** A dedicated node keeps `Loop`'s collection semantics
   clean and makes the digest/analysis explicit. Whichever is chosen must fold into `Program.Digest`
   so the run-start/resume drift hash (#277) sees a lowering change.
3. **`limit` vs the global cap (J4).** Prefer rejecting `limit > DefaultMaxLoopIterations` at check
   time; otherwise fail loudly before executing. The per-loop bound is the semantics of the
   construct, not the global cap.
4. **Condition-divergence contract (J5).** A condition that flips between the original run and a
   resume must be a loud error, never a silent different iteration history. Extend the control-record
   only as far as needed and test the failure path explicitly.
5. **J6 design choice.** Confirm the Tool operation manifest (#204 `Operations`) gives a closed world
   to validate multiple advertised operations against before choosing design 1 over 2.

## Suggested order

J1 → J2 (authoring surface) · J3 → J4 → J5 (bounded, durable `while`) · J6 and J9 (capability +
typed I/O, independent) · then J7 (flagship, consumes J2/J5/J6/J9) · J8 last as a follow-up.
`docs/LANGUAGE.md` and `docs/EXAMPLES.md` are updated by the issues as each lands.

The one normative decision this epic introduces — *no unbounded effectful loop; the `while` bound is
mandatory, runtime-enforced, and orthogonal to the effect bound* — is recorded up front in
[ADR 002 §6](../adr/002-language-frontend-and-ir-expressiveness.md) (marked decided/pending, Epic H2).
The additive pieces (instructions, multiline strings, multi-operation grants, typed I/O) are not
ADR-level decisions and stay in `LANGUAGE.md`.
