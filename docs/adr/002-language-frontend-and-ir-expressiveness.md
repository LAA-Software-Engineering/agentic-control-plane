# ADR 002: Language frontend and IR expressiveness

## Status

Accepted (2026-08-17)

## Context

[`docs/DESIGN_DOC.md` §7.4](../DESIGN_DOC.md) ("End goal additions", lines 746–753) commits the
project to eight workflow capabilities beyond the sequential MVP:

```text
conditional steps · loops · fan-out/fan-in · scheduled triggers
event triggers · human approval steps · parallel branches · subworkflows
```

None of the eight is scheduled. Across all issues, open and closed, not one proposes any of
them; every open issue belongs to Epics A–E of `docs/IMPROVEMENT_SPEC.md`, and issue #22 is
titled "Sequential workflow engine core". §7.4 is therefore a standing wishlist with no owner
and no design.

A recurring external suggestion is that ACP should acquire a **language frontend** — a
`.agent` surface syntax compiled into the existing resource model as an IR — on the argument
that the resource graph already resembles a compiler IR and the YAML pipeline already resembles
a compiler pipeline.

### What the IR can represent today

| Construct | Representable? | Evidence |
|---|---|---|
| Nested call expressions | Yes, by SSA-flattening to steps + `${steps.x}` refs | [`internal/engine/interpolation.go`](../../internal/engine/interpolation.go) |
| Named arguments, bindings, return value | Yes — `with:`, step IDs, `output.value` | [`kinds.go:176`](../../internal/spec/kinds.go) |
| Agent invocation with inputs | Yes — `agent:` + `with:` | `kinds.go:176` |
| **Parallel branches / fan-in** | **No** — execution is `for _, step := range wf.Spec.Steps` | [`internal/engine/execution.go:304`](../../internal/engine/execution.go) |
| **Checked types across steps** | **No** — interpolation stringifies every value; schemas are validated for file existence only | `interpolation.go`, [`validator.go:435`](../../internal/spec/validator.go) |
| **Subworkflow calls** | **No** — `WorkflowStep` has four fields: `ID, Uses, Agent, With` | `kinds.go:176` |
| **Call-site approval** | **No** — approvals are resource-level `approvals.requiredFor` | [`internal/policy/approvals.go`](../../internal/policy/approvals.go) |

The decisive observation: **a frontend can only compile to primitives the IR already has.**
Roughly half of any proposed surface syntax is free desugaring; the other half is IR work that
must happen whether or not a parser is ever written. Surface syntax does not supply
parallelism or a type system — it presupposes them.

### What is actually differentiated

The resource graph plus the policy engine is one step away from a **capability/effect system**.
[`internal/policy/derive.go`](../../internal/policy/derive.go) already performs fail-closed
safety derivation from `ToolSafety`. Once Epic A lands and agents autonomously select tools,
the transitive effect set of a workflow becomes a **sound static upper bound on the blast
radius of a nondeterministic agent**, computable at plan time and reviewable as a diff.

No adjacent tool (LangGraph, OpenAI Agents SDK, Temporal) can produce that, because it requires
the declared-resource graph. It requires **no new syntax**.

## Decision

### 1. The language frontend is the committed implementation strategy for the computational subset of §7.4 — not a rejected idea, and not the next build.

§7.4's eight bullets are ruled on individually:

| §7.4 bullet | Classification | Implementation |
|---|---|---|
| parallel branches | graph structure | YAML / IR |
| subworkflows | graph structure | YAML / IR |
| human approval steps | graph node | YAML / IR |
| scheduled triggers | trigger metadata | YAML / IR (`WorkflowTrigger`) |
| event triggers | trigger metadata | YAML / IR (`WorkflowTrigger`) |
| **fan-out/fan-in** | **splits — see below** | both |
| **conditional steps** | **computation** | language frontend |
| **loops** | **computation** | language frontend |

**Rule:** graph structure is declarative and belongs in the IR. Computation — anything
requiring an evaluated expression — does not go into YAML. A dependency graph reads fine in a
declarative format (Argo, `needs:` in GitHub Actions). Conditionals and iteration bounds do not,
and the failure mode is well documented in CloudFormation and GitHub Actions.

**Fan-out is the trap.** *Static* fan-out — N branches known at author time — is parallel
branches and belongs in the IR. *Dynamic* fan-out — iterating a runtime collection
(`foreach items: ${steps.x.files}`) — requires a bound expression and is a loop wearing a graph
costume. It will arrive labeled as graph work. It is language work.

### 2. Ordering: effects first, IR expressiveness second, frontend last.

1. **Effect system** (Epic F). Declared effects on tools, transitive effect bounds over the
   project graph including autonomous agent edges, enforcement against `Policy`, surfaced in
   `plan`. Highest differentiation per line of code, no new syntax, shippable against the
   current YAML surface.
2. **IR expressiveness** (Epic G). The five graph-shaped bullets. Each is independently
   valuable to YAML authors and each is a prerequisite for the frontend regardless.
3. **Frontend** (Epic H). Shipped when it can execute — by then largely a lexer, a parser, and
   a lowering pass.

### 3. The normative target surface is fixed now.

Epic G issues are specified against this program. Each IR change has a concrete acceptance
test: *can the IR represent this line yet?*

```text
agent Reviewer {
    model  openai/gpt-5
    policy guarded-writes        // ref to a Policy resource; `strict` is its preset base
    grants { github.read }       // autonomous capability bound — NOT a call list
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

Four properties of this surface are normative:

- **The `effects` clause is checked, and the check covers autonomous tool selection.** The
  compiler proves the body cannot exceed the declared set even though `Reviewer` picks its own
  tools at runtime. Adding `destructive` to a tool fails compilation for every workflow that can
  transitively reach it until a human widens the clause in a reviewable diff. This is the
  distinguishing feature of the language and the reason it is worth building.
- **No call-site `approve` statement.** Approval gates stay in `Policy` resources so that
  approval scope remains an independently diffable artifact. Inlining `approve` into program
  text couples the gate to the behavior and destroys the property that `plan` can report
  "approval scope widened" without the workflow changing.
- **No anonymous functions.** Every callable is a declared resource with an effect set. An
  undeclared function is an unbounded one, and unbounded callables make the effect analysis
  unsound.
- **`grants` is an autonomous capability bound, not a call list.** Post-Epic A, listing a tool
  on an agent means the agent *may choose to call it*. That is a distinct concept from a
  workflow-level deterministic invocation, and the two must not share syntax.

### 4. No control-flow field lands on `WorkflowStep` without superseding this ADR.

Specifically: no `if`, `when`, `condition`, `foreach`, `loop`, `while`, or `expression` field on
[`WorkflowStep`](../../internal/spec/kinds.go). `WorkflowStep` is a four-field struct today; a
fifth is an afternoon's work and permanent. Graph-structure fields (`needs`, `parallel`,
`workflow`, `approval`) are explicitly permitted and are the subject of Epic G.

## Consequences

- **Positive:** §7.4 stops being an unowned wishlist. Each bullet has a ruling, so the next
  contributor has a decision to follow rather than a list to interpret.
- **Positive:** The effect system ships years before any parser and is the strongest available
  argument for why the declarative model earns its cost — the "Product finding" gap named in
  `docs/IMPROVEMENT_SPEC.md` §0.
- **Positive:** Epic G work is justified on its own terms to YAML authors; none of it is
  speculative investment in a language that may not ship.
- **Negative:** Conditionals and loops are unavailable until Epic H. Workflows needing them must
  push the branch inside an agent (which is legitimate — an agent choosing its own path is the
  autonomous case) or decompose into multiple workflows.
- **Negative:** Fixing the target surface now risks over-fitting the IR to a syntax that may
  change. Mitigated by keeping Epic G issues specified in terms of *semantics* (concurrent
  execution with fan-in; typed values crossing step boundaries), not tokens.
- **Reversal cost:** Low before Epic G lands, high after. If the frontend is abandoned, Epic F
  and G remain fully valuable. If the frontend is accelerated, Epic G is still the prerequisite.

## Dependency

This ADR assumes **Path 1** from `docs/IMPROVEMENT_SPEC.md` §0 — that agents genuinely select
tools autonomously (Epic A). Under Path 2 ("reframe as a declarative governance layer for LLM
tool pipelines"), the effect bound degenerates to a static call-graph walk, still useful but no
longer distinguishing, and the ordering argument in §2 above should be revisited.

## References

- ADR 001 — control plane vs runtime boundary
- ADR 003 — YAML as compilation output
- `docs/DESIGN_DOC.md` §7.4 (end goal additions), §20 (implementation phases)
- `docs/IMPROVEMENT_SPEC.md` §0 (positioning decision), Epic A
- Issue #22 — sequential workflow engine core
- Issue #160 — bounded agent tool-calling loop (Epic A)
