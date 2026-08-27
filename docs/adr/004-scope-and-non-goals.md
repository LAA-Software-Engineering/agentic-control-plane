# ADR 004: Scope boundary and non-goals

> Terfyn exists to make the authority of nondeterministic programs **statically bounded,
> reviewable before execution, and invariant across the execution lifecycle.** Everything
> else has to justify itself against that sentence.

## Status

**Proposed** (2026-08-27).

The **non-goals list below is a draft to attack, not a settled boundary.** The admission test and
the shape of the argument are the decision being proposed; the specific forbidden systems are a
starting position for the maintainers to redline. An ADR draft exists precisely to be concrete
enough to argue with.

## Context

Terfyn has grown from "a declarative control plane for agent systems" into something with a much
sharper center: **a statically analyzable, capability-oriented execution platform for
nondeterministic programs** (the framing the README now leads with). Across 400+ commits it has
accreted a configuration/control plane, a compiler frontend (`.agent`), a type system, an effect
system, a capability-security model, a policy engine, an execution IR, a workflow runtime,
durability/checkpointing/HITL, content-addressed deployment snapshots, a schema system, tool
runtimes (native/HTTP/MCP), a model-provider abstraction, and a tamper-evident audit chain. The
[execir-on-engine epic (#255)](../plans/255-execir-engine.md) adds a durable interpreter with
memoized replay.

That is a lot of surface. The healthy surprise is that a single sentence explains why almost every
piece belongs (see the top of this document), and each apparently-baroque subsystem *follows* from
it:

- Content-addressed snapshots ([ADR follow-through, #207](../DESIGN_DOC.md)) are not infrastructure
  for its own sake — without them a suspended run could wake up under a newly widened policy.
- The capability manifest (#204) is not feature creep — without it a live MCP `tools/list` destroys
  the closed-world assumption the effect bound depends on.
- The execution IR is not language astronautics — `.agent` has branches and loops, but the
  reviewable resource graph deliberately does not (ADR 002 §4/§5).
- The effect system is not decoration — it is the mechanism that makes authority reviewable.

The threat at this size is no longer implementation quality. It is **scope gravity**: adjacent
systems (a vector store, a secrets manager, an observability backend, a CI platform) can each be
argued to "help," and a project without a stated boundary accretes them until the center is diluted
and the maintainers own a junk drawer. This ADR draws the boundary while the center is still legible.

The companion to this document is [`docs/SOUNDNESS.md`](../SOUNDNESS.md): ADR 004 governs *what
Terfyn is allowed to become*; SOUNDNESS.md governs *whether the central claim stays true* as it
grows. Scope creep is visible and slow; a broken soundness invariant is invisible and ships green.
The second is at least as important as the first.

## Decision

### 1. The center

Terfyn's product is **the guarantee that reviewed authority is executed authority.** Static effect
bounds are the *mechanism* that makes authority reviewable; content-addressed snapshots and
pinned enforcement are the mechanism that makes the reviewed authority the *executed* authority. The
effect system is not the product — an impressive effect bound attached to a runtime that can
accidentally re-read current policy, current schemas, or newly discovered MCP operations is
sophisticated security theater.

Terfyn owns the mechanisms **necessary** to:

1. **statically bound** the authority a nondeterministic program may exercise,
2. **review** changes to that authority before execution,
3. **reproduce** the reviewed deployment state, and
4. **faithfully execute** the program under exactly that state.

### 2. The admission test

A proposed subsystem belongs in Terfyn **only if removing it would materially weaken one of:**

1. static analyzability,
2. authority review,
3. deployment reproducibility, or
4. faithful execution of reviewed authority.

Necessity, not benefit. Almost anything can be argued to *strengthen* something indirectly ("a
vector store would help agents retrieve better, which improves outcomes…"); the bar is whether the
central claim is *materially weakened without it*. Necessity is harder to bullshit.

### 3. In scope (owned)

Each of these fails the "remove it" test — take it away and one of the four pillars collapses:

| Subsystem | Pillar it is necessary for |
|---|---|
| Effect system, type system, `.agent` checker | static analyzability |
| `plan` authority delta, capability manifests, policy engine | authority review |
| Content-addressed deployment snapshots, schema capture, pinned digests | deployment reproducibility |
| Execution IR, workflow runtime, durable replay/checkpoint/HITL, pinned enforcement | faithful execution |
| SQLite state, tamper-evident audit chain | review + reproducibility (the record of what was reviewed and what ran) |
| **Adapters** to models, tools (native/HTTP/MCP), and runtime targets | the boundary at which Terfyn touches the outside world |

### 4. Non-goals (integrate, do not own)

Terfyn **may integrate with** each of the following through an adapter at its boundary. Terfyn does
**not become** them — none is necessary to the four pillars, and owning it dilutes the center and
imports a maintenance surface whose correctness has nothing to do with authority. *(Draft list —
redline it.)*

| System | Adapter (yes) | Ownership (no) |
|---|---|---|
| Model serving / hosting | call a hosted or local model endpoint | run a model-serving stack |
| Vector store / RAG platform | a tool that queries an external store | implement/host the store or retrieval pipeline |
| Secrets management | resolve `env:`/reference-style secrets at request time (already the design) | a general secrets manager / vault |
| Observability backend | export OTel spans + the audit chain to an external system | store/query/dashboard telemetry as a product |
| CI/CD | be *invoked by* CI (`validate`/`plan`/`apply` in a pipeline) | a general CI/CD platform |
| Collaboration / review UI | emit reviewable `plan` diffs for humans and tools to consume | a hosted collaboration product |
| Agent / plugin marketplace | declare tools/agents in a project | a distribution marketplace |
| Cluster / fleet management | a runtime adapter targeting a cluster | a Kubernetes-style orchestrator |
| Distributed task queue | durable execution *of the reviewed program* (execir) | a general-purpose task queue |
| General-purpose programming language | the computational subset in `.agent` (ADR 002 §4 bounds it deliberately) | an unbounded language (arbitrary I/O, `while`, unrestricted expressions) |
| Agent-memory framework | a tool/store an agent calls | an elaborate memory subsystem with its own semantics |

The design doc already gestures at this line — models, tools, policies, workflows, and execution are
inside; remote runtimes, memory stores, and modules are marked as later *extensions* — but states it
as a roadmap, not a boundary. This ADR states it as a boundary.

### 5. The honesty boundary is itself a non-goal

Terfyn bounds and diffs the **grant** of authority. It does **not** verify what a remote system
(GitHub, a shell, an MCP server) actually *does* when invoked (ADR 002, *Soundness assumptions and
limits*). The trust anchor is human review of the manifest and effects, not runtime verification of
remote semantics. **Formal verification of remote behavior is a non-goal**, and documentation must
not claim it — that distinction is what keeps Terfyn out of formal-verification hell and its claims
defensible.

### 6. The Authority TCB

Not every package is equally security-sensitive. A small set of code determines whether the central
claim is true — Terfyn's **Trusted Computing Base for authority**. See
[`docs/SOUNDNESS.md`](../SOUNDNESS.md) for the enumerated packages and the raised review standard.
Admitting a subsystem that enlarges the TCB without being necessary to the four pillars is the most
expensive kind of scope creep, because it grows the surface on which the product claim can silently
become false.

## Consequences

- **Positive:** contributors and maintainers get a *necessity* test to apply, not a taste argument.
  "Would removing this materially weaken static analyzability / authority review / reproducibility /
  faithful execution?" is answerable; "would this be nice?" is not.
- **Positive:** Terfyn can keep growing without becoming a junk drawer. The center stays legible.
- **Positive:** pairs with SOUNDNESS.md so the boundary (what it becomes) and the invariants (whether
  it stays true) are governed together.
- **Negative:** the non-goals list will feel arbitrary at the margins (is a first-class secrets
  integration an "adapter" or "ownership"?). That is expected; the admission test, not the list, is
  the durable artifact. The list is a draft to be argued down.
- **Negative:** some genuinely useful adjacent capability will be declined. That is the point of a
  boundary; the cost of *not* having one is worse.
