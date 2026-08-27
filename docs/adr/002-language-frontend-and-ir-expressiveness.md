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

A recurring external suggestion is that Terfyn should acquire a **language frontend** — a
`.agent` surface syntax compiled into the existing resource model as an IR — on the argument
that the resource graph already resembles a compiler IR and the YAML pipeline already resembles
a compiler pipeline.

### What the IR can represent today

| Construct | Representable? | Evidence |
|---|---|---|
| Nested call expressions | Yes, by SSA-flattening to steps + `${steps.x}` refs | [`internal/engine/interpolation.go`](../../internal/engine/interpolation.go) |
| Named arguments, bindings, return value | Yes — `with:`, step IDs, `output.value` | [`kinds.go:176`](../../internal/spec/kinds.go) |
| Agent invocation with inputs | Yes — `agent:` + `with:` | `kinds.go:176` |
| **Parallel branches / fan-in** | **Yes** — `WorkflowStep.needs` (issue #192); execution is a bounded DAG in [`internal/engine/execution.go`](../../internal/engine/execution.go) |
| **Checked types across steps** | **No** — interpolation stringifies every value; schemas are validated for file existence only | `interpolation.go`, [`validator.go:435`](../../internal/spec/validator.go) |
| **Subworkflow calls** | **No** — `WorkflowStep` is `ID, Uses, Agent, With, Needs` | `kinds.go` |
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

**Rule:** the **author-facing Workflow resource does not acquire an expression language**.
Computational syntax belongs exclusively to `.agent`; lowering may introduce internal
execution-IR constructs (see §5). Graph structure is declarative and belongs in the resource
model — a dependency graph reads fine in a declarative format (Argo, `needs:` in GitHub
Actions). Conditionals and iteration bounds do not, and the failure mode is well documented in
CloudFormation and GitHub Actions.

The rule is deliberately about the *authoring surface*, not about all IR. A compiler needs
somewhere to put an `if`; it must not be a field a human writes.

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
    grants {                     // autonomous capability bound — NOT a call list
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
- **Grants name concrete operations; effects name classes of consequence.** These are separate
  namespaces and must never be interchangeable — see below.

### Capabilities and effects are distinct namespaces

A **grant** answers *which concrete operation may this agent invoke?* An **effect** answers
*what class of observable consequence can that operation have?* They are related by the
per-operation declarations in Epic F, and the relation is one-directional:

```text
grant  tool.github.read_pr
         ↓ (operation's declared effects)
       { github.read }
         ↓ (union over grants and static calls)
       workflow effect bound
```

Grants use the existing reference vocabulary — `tool.<name>.<operation>`, the same form already
used by `approvals.requiredFor` in every shipped example. Effects use bare dotted identifiers
(`github.read`, `destructive`). The `tool.` prefix keeps the two textually distinguishable.

**Grants must never be written in terms of effects.** If `grants { github.read }` meant "every
operation classified `github.read`", then adding a new operation to a tool and classifying it
`github.read` would silently widen every granting agent's concrete capability, with no diff on
the agent and possibly no change in the effect delta. That is precisely the invisible capability
widening this system exists to expose.

Named capability groups may be introduced later for ergonomics, but a group **resolves to a
concrete operation list**, and `plan` renders the expanded list rather than the group name.
Expansion is the reviewable artifact.

### 4. No control-flow field lands on `WorkflowStep` without superseding this ADR.

Specifically: no `if`, `when`, `condition`, `foreach`, `loop`, `while`, or `expression` field on
[`WorkflowStep`](../../internal/spec/kinds.go). `WorkflowStep` is a four-field struct today; a
fifth is an afternoon's work and permanent. Graph-structure fields (`needs`, `parallel`,
`workflow`, `approval`) are explicitly permitted and are the subject of Epic G.

This constrains the **resource model** specifically, because that is what humans author, what
`plan` diffs, and what deployment state stores. It does not constrain the execution IR.

### 5. Resource IR and execution IR are sibling projections, not a pipeline.

Conditionals and loops must become *something* after the AST is discarded. Stating only "no
control flow in the IR" leaves the compiler nowhere to lower to.

The naive reading — `AST → resource IR → execution IR` — is **wrong and must not be
implemented.** The resource IR cannot represent `if` or a loop by decision 4, so an execution IR
containing `Branch` and `Loop` cannot be derived from it. A linear pipeline would force the
computational information to be smuggled into the resource IR, which is exactly what decision 4
forbids.

Both are projections of one checked program:

```text
.agent  →  typed AST  →  checked program
                         (resolved refs · types · effect bound)
                                  │
                ┌─────────────────┴──────────────────┐
                ↓                                    ↓
      resource projection                    execution lowering
      Agent · Tool · Policy                  InvokeTool · InvokeAgent
      Workflow identity + graph              InvokeWorkflow · Fork · Join
      Environment · effect graph             Branch · Loop · Return
                ↓                                    ↓
        plan / apply / state                       engine
```

- **The checked program** is the single source of truth after type and effect checking (#198).
  Neither projection is authoritative over the other.
- **Resource projection** is the existing resource model — desired state: what `plan` diffs, what
  `apply` writes, what policy and effect analysis run against. It never gains an expression
  language.
- **Execution lowering** produces the executable form. `Branch` and `Loop` live here and only
  here. Never hand-authored, no YAML surface.

**YAML ingress is a degenerate case, and that asymmetry is intended.** For YAML the resource IR
*is* the checked program — there is no separate representation, because YAML cannot express
anything the resource model cannot hold. Execution lowering runs directly from it. Do not force
YAML through a synthetic "program" layer that adds nothing; do require both paths to converge on
the same execution IR, since divergent execution semantics between ingress paths is a defect, not
a design freedom.

Three consequences that must hold or the split is meaningless:

- **`plan` diffs the resource projection.** The execution IR produces no diff lines of its own.
  Its digest folds into the workflow hash so a lowering change with no resource-level change still
  invalidates a stale plan.
- **`apply` persists the execution IR, not just the resource projection.** This follows from
  [ADR 001](001-control-plane-runtime-boundary.md), which forbids runtime adapters from calling
  `project.LoadProject` or re-reading user YAML: the runtime executes from a resolved snapshot
  only. Recompiling `.agent` from source at run time is therefore already ruled out, and the
  resource projection alone cannot reconstruct `Branch`/`Loop`. The compiled execution IR is a
  deployment artifact, stored alongside the resource state, following the existing
  `.agentic/resolved-config.json` snapshot pattern (#112).
- **A run pins one immutable deployment snapshot for its entire lifetime.** The execution IR is
  one artifact under that snapshot, not a separately pinned thing. A pinned digest only lets the
  system *detect* change; it cannot reproduce what ran, so the artifacts must be retained,
  content-addressed, and never mutated by a later `apply`.

  The primitive is the **snapshot**, not a growing set of digest columns on `runs`. Pinning the
  execution IR and capability manifest individually would not fix the bug that motivates this:
  [`ResolvedConfigForRun`](../../internal/runtime/local/resume_validate.go) re-resolves from
  *current* config with only the environment name pinned, so a policy edit between suspend and
  resume is picked up silently — `workflow_spec_hash` covers the workflow alone. Each additional
  pinned dimension would be another column and another way to miss one.

  [`config.ResolvedConfig`](../../internal/config/resolved.go) is the right *abstraction* already
  — it holds the whole resolved `ProjectGraph` and exposes a `Digest()` — but it is not yet a
  persisted artifact: `.agentic/resolved-config.json` stores only `{digest, environment}` for
  plan→run contract checks (#112). #207 must therefore add a canonical serializable resolved-graph
  artifact, immutable retention, content addressing, and the run→snapshot reference. The snapshot
  digest must also be defined independently of `ResolvedConfig.Digest()`, which mixes in the
  absolute `statePath` — where the database happens to live is not part of a program's executable
  configuration. Tracked as #207.

  Resulting invariant:

  > A run executes against exactly one immutable resolved deployment snapshot for its entire
  > lifetime.

This does not require separate Go type hierarchies on day one, but the projection relationship is
normative and the boundary should be visible in package structure.

## Soundness assumptions and limits

This ADR claims a "sound static upper bound on the blast radius of a nondeterministic agent."
That claim is conditional, and the conditions must be stated or the claim is false.

### The effect bound requires a closed world

A static bound over reachable operations is sound only if the set of callable operations cannot
grow after the bound is computed. Terfyn does not currently guarantee that.
[`internal/tools/mcp_safety.go`](../../internal/tools/mcp_safety.go) calls `tools/list` at config
resolution and merges remote-advertised `meta.mcp_flags` into `spec.safety`; a remote MCP server
that advertises `read_issue` and `list_pr` at plan time may advertise `delete_repo` tomorrow. The
existing discovery warning already tells authors to "pin `spec.safety` in YAML for stable
plan→run digests" — a manual workaround for a known instability, with no enforcement.

**Required invariant:** *no operation may become agent-callable unless it was present in the
deployed capability manifest.*

Each `Tool` resource therefore carries an allowed-operation manifest with per-operation effects
(`spec.operations`), derived by `tools.DeriveManifest`. Runtime `tools/list` may return anything;
operations absent from the deployed manifest are denied on the policy path (`CheckToolCall` →
`operation_not_in_manifest`, exit 5, traced), in **both** the compiled snapshot evaluator that
`terfyn run` uses and the legacy evaluator, before any `Permissive`/`DecisionAllow` short-circuit.
Manifest drift — an operation appearing, disappearing, or changing effects — surfaces as a Tool
state change in `plan` because `spec.operations` is part of the Tool's normalized spec hash (the
existing pin, per the note above about not adding a second pinning mechanism), rather than silently
expanding the callable set. This is tracked as a first-class Epic F issue (#204).

**Shipped by #204:** the desired/deployed manifest model, `validate`/`plan` bounding over the
desired manifest, and runtime closed-world denial of operations outside the declared manifest.
Closedness is a presence bit (`operations: {}` is a closed, deny-all world; an omitted key is open
and backward compatible), not the operation count, so shrinking a manifest cannot reopen it. The
bit is part of identity (`ToolSpec.OperationsDeclared`, serialized into the normalized spec), so
plan, apply, and the deployed manifest reconstructed from applied spec see the same closed world
runtime enforces — deleting `operations:` from a locked tool is a visible plan change, not a silent
reopen. YAML interchange preserves it as well (ADR 003): `ToolSpec.MarshalYAML` emits an explicit
`operations: {}` for a declared-but-empty manifest so `terfyn export` → load does not drop it.
Discovery merges only `spec.safety` and never adds operations, so it is never an authority source.
`tools.CapabilityManifest.Digest` / `GraphManifestDigest` are manifest-identity primitives for
comparison and the #207 run-pin, not a second plan/apply pin. Each operation also declares an input
schema (`operations.<op>.schema`, the "operation → effects → schema" the manifest describes): the
ref is part of manifest identity, `validate` compiles it, a tool call's input is validated against
it before dispatch, and its content is captured into the #207 schema bundle so a pinned resume
enforces the schema it started with.

**Run-pinned authority (shipped by #207):** a run pins its deployment snapshot
(`runs.deployment_snapshot_digest`), and `run --resume` hydrates the resolved graph from that
snapshot and compiles the policy from it (not from the on-disk policy snapshot `apply` overwrites),
so a suspended run enforces the policy *and* manifest it started with — approvals, presets, and
safety-derived `CheckToolCall` decisions included, not only manifest membership.

### Three kinds of manifest, and one authority per phase

Conflating these makes `validate` impossible to define for a project that has never been applied
— there is no deployed manifest yet, so there is nothing to bound against. Keep them distinct:

| Concept | Meaning | Authority? |
|---|---|---|
| **Remote discovery** | what a server currently advertises (`tools/list`) | **Never.** Observation only — it populates a *desired* manifest during authoring and is never trusted at run time |
| **Desired manifest** | what the proposed configuration *will* permit | Proposed authority |
| **Deployed manifest** | what the applied configuration *does* permit | Actual authority |

Each phase resolves against exactly one:

```text
validate   bound(desired)
plan       bound(desired)  vs  bound(deployed)      → authority delta
apply      desired becomes deployed
run        enforce(deployed, as pinned on the run)
```

The `plan` comparison is what produces the reviewable authority diff — and the distinction
between a widened *static* and a widened *autonomous* bound is the highest-value line of output
in the system (#191).

A resumed run enforces the manifest **pinned at run start**, not whatever is currently deployed.
Otherwise an `apply` landing mid-run silently changes the authority of an in-flight
nondeterministic agent, which is the precise failure this whole section exists to prevent. The
resulting invariant:

> An in-flight nondeterministic program cannot acquire new authority merely because the control
> plane changed underneath it.

This requires a retained, run-pinned deployment snapshot, not just pinned digests — **shipped in
#207**: `apply` and run-start retain immutable, content-addressed artifacts (resolved graph +
capability manifest) rooted by a `deployment_snapshots` row, and `run --resume` hydrates authority
from the run's pinned snapshot. Reproducibility limits still apply (below): a snapshot reproduces
the *configuration* a run executed under, never the *execution*.

**Reproducibility has the same kind of limit as soundness.** A snapshot reproduces the
*configuration* a run executed under — graph, policy, manifest, lowered program. It does not
reproduce the *execution*: model weights, sampling nondeterminism, and remote API behavior are
outside it. Claim configuration reproducibility and authority provenance; do not claim replay.

**Accepted trade-off.** A run resumed against a superseded manifest is executing something that is
no longer desired state. That is the right default, because the alternative strands
approval-gated work: under a refuse-on-drift rule, any unrelated `apply` would permanently kill a
run suspended at a HITL gate. But the divergence must be *visible* — `inspect` and `logs` should
show that a run is executing a superseded artifact rather than presenting it as current.

### The bound is over the callable set, not over callable behavior

Even with manifest pinning, the guarantee is narrower than it first appears:

> No operation outside the deployed manifest becomes callable, and each operation's effects are
> as declared and reviewed.

The trust anchor is **human review of the manifest**, not runtime enforcement of semantics. If
`tool.github.read_pr` declares `github.read` but its `ToolHTTP.baseUrl` points elsewhere, or the
remote API changes what that endpoint does, the declaration is a lie and no analysis catches it.
`ToolHTTP` carries exactly the same exposure as MCP.

Documentation and marketing must not overstate this. The defensible claim is that Terfyn bounds and
diffs the *authority* granted to nondeterministic components — which is genuinely more than any
comparable tool offers — not that it verifies what remote systems do with that authority.

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
