# Implementation plan — #255: Execute the execution IR (`execir`) on the engine

Status: planned (rev. 2 — corrected after review). Epic [#255]. Builds toward the ADR 002 §5
convergence goal. Depends on #199 (execir + `.agent` lowering, merged) and #207 (deployment snapshot
with the reserved-but-empty `execution_ir_digest`, merged).

> **Revision note.** Rev. 1 made three load-bearing claims that were false against the current
> runtime, and would have failed the first feature built on them. They are corrected here and called
> out inline: (a) the durable resume grain is **not** memoization of completed `Invoke*` leaves; (b)
> YAML `needs:` is a **general DAG**, not `execir.Fork`'s structured fork-join; (c) pinning the
> program is a **prerequisite** for running control flow, not a low-risk follow-up. The `.agent`
> execution IR is also **already constructed** on the load path (the result is dropped), so Phase
> "pin" plumbs an existing pass rather than inventing a lowering.

## Goal

Make the workflow run path **execute `execir`** ([`internal/execir`](../../internal/execir)) —
first at parity with today's `WorkflowStep` DAG runtime, then to enable `.agent` control flow
(`if`/`for`/`parallel for`) end-to-end, with the lowered program **pinned into the deployment
snapshot before it is ever executed**. This closes ADR 002 §5's convergence target: both ingress
paths (`.agent` and YAML) share one interpreter, so execution semantics cannot diverge.

## Where things stand

- **`execir` is a complete, isolated interpreter.** [`execir.go`](../../internal/execir/execir.go)
  defines `Program` (params + a tree of `Node`s: `InvokeTool/Agent/Workflow`, `Let`, `Branch`,
  `Fork`, `Loop`, `Return`); [`interp.go`](../../internal/execir/interp.go) walks it against an
  injected [`Invoker`](../../internal/execir/interp.go). It is deliberately **runtime-independent**:
  no checkpoint/resume, HITL, policy, cost, trace, or telemetry. **Two gaps the interpreter has by
  construction, load-bearing for later phases:** the `Invoker` ABI is
  `InvokeTool(ctx, uses, args)` — it carries **no structural key** (no node path, loop index, or
  step id); and there is **no `Approval` node**, so a `#195` graph-node pause has nothing to lower to.
- **The engine is a rich DAG runtime.**
  [`execution_dag.go`](../../internal/engine/execution_dag.go) schedules `wf.Spec.Steps` by `needs`
  (`depsReady`) with bounded concurrency — a step runs as soon as *its* predecessor set is complete;
  independent roots run concurrently; a join lists exactly the branches it named; HITL in one branch
  does not unschedule a sibling whose `needs` are already satisfied (`#195`). It layers on
  per-step checkpointing, resume, HITL suspend (`ErrInterrupted`), policy, cost/budget, limits, and
  trace + OTel. The checkpoint payload is `checkpointPayload{Steps, Completed, PendingHitl, Nested}`
  ([`checkpoint.go`](../../internal/engine/checkpoint.go)) — note the last two.
- **Construction of the `.agent` execution IR already runs on the load path; only its *result* is
  dropped.** [`compileAgentSources`](../../internal/project/agent_sources.go) calls
  [`check.Check`](../../internal/lang/check/check.go), which calls
  [`lower.LowerExec`](../../internal/lang/lower/exec.go) for every workflow and stores the result on
  `Program.Executables` (positional-arg rebinds included). The loader keeps `prog.Graph` (the
  resource projection) and **discards `Executables`**. Likewise
  [`plan.WorkflowSpecHash`](../../internal/plan/workflow_hash.go) *is*
  `WorkflowSpecHashWithExec(wf, "")` — the fold function is the production hash path; only the digest
  argument is empty. So for `.agent`, the "pin" phase **plumbs an existing pass**, not a new lowering.
- **What is genuinely unwired.** No run path *executes* an `execir.Program`;
  [`deploy.Build`](../../internal/deploy/snapshot.go) hardcodes `ExecutionIRDigest: ""`;
  `ResolvedConfig` has no `Executables` field; and
  [`project.controlFlowGate`](../../internal/project/agent_sources.go) *refuses* `.agent` `if`/`for`
  workflows at load — because removing it would leave the DAG executing `LowerFile`'s **flattened
  arms** (a sound *effect* over-approximation, an unsound *execution*).

## The crux: durable execution is not leaf memoization

**Corrected (rev. 1 was wrong here).** The engine's resume is *step-ID/data-based*
([`loadResumeState`](../../internal/engine/checkpoint.go)); `execir` is a nested tree. A naive
"Temporal-style memoization of completed `Invoke*` leaves, `{key → result}` + cost" — as rev. 1
stated — **cannot pass the `#105/#106/#192/#195` suites** #258 must port, for three concrete reasons:

1. **The suspend point is an incomplete workflow-level node, not a completed leaf — and it is *not*
   inside the agent loop.** HITL fires at a workflow-level `uses:` step (`maybeInterruptForHitl`,
   the single call site in [`execution_dag.go`](../../internal/engine/execution_dag.go), *before*
   `runToolStep`) or a `#195` approval step. The **inner agent-loop does not HITL** — a granted
   operation `CheckToolCall` denies for a missing `--approve` fail-closes with **exit 5**, not a
   gate (`docs/AGENT_LOOP.md` §"HITL vs exit 5"; DESIGN_DOC §12.2 F). So there is *no agent-loop
   continuation to carry*: `InvokeTool`/`InvokeAgent` are leaves — completed → memoized by
   structural key; an incomplete one (a crash mid-agent-step) replays the whole step, which is
   exactly today's DAG behavior. The real in-flight states are the ones the engine already stores:
   `checkpointPayload.PendingHitl` (an incomplete `uses:` / `Approval` node paused at a gate) and
   `checkpointPayload.Nested` (a suspended subworkflow, `#194` `NestedRunState`). These are
   *incomplete* nodes — not memoizable as completed-leaf results — so the durable model must
   *extend* `PendingHitl` + `Nested` (+ the `Approval` node) onto execir node identities, not
   *replace* the checkpoint with a `{key → result}` map.
2. **The `Invoker` ABI cannot see a key.** Memoizing "node path + enclosing loop indices" requires
   the key to reach the invocation. `InvokeTool(ctx, uses, args)` does not carry it, and stashing a
   "current node" on the adapter is unsafe — `Fork` and parallel `Loop` invoke from several
   goroutines. **The ABI must be extended** (key/identity passed per call, or via a per-branch
   context), goroutine-safe by construction.
3. **Approval is a node, not a leaf.** `#195` approval steps are a fourth XOR step kind (see
   concurrency section); a graph-node pause is not "a leaf hitting an approval gate." execir needs
   an **`Approval` node** whose suspend/resume is modeled explicitly.

**Corrected model.** Durable execir = deterministic replay of pure control flow (`Branch`
conditions, `Let`, collection sizes — pure by construction, #199) + memoized **completed** effectful
leaves keyed by a stable structural identity + **the engine's existing in-flight interrupt state**
— `PendingHitl` (incomplete `uses:` / `Approval` node) and `Nested` (suspended subworkflow) —
carried across suspend as `checkpointPayload` does today, re-anchored to execir node identities.
There is deliberately **no** agent-loop program counter: the inner loop does not suspend, and an
interrupted agent step replays wholesale (existing behavior). The #258 sign-off must resolve all
three sub-problems above; "replay-with-memoization" alone is a slogan that stops one layer short of
the interrupt states the engine actually models.

## The concurrency model: `needs:` is a general DAG, not `Fork`

**Corrected (rev. 1 called this "low risk / isomorphic"; it is neither).** The engine schedules an
arbitrary DAG. `execir.Fork` starts every branch together, waits for **all**, and publishes only
`ForkBranch.Bind` — it encodes **series-parallel** graphs. YAML `needs:` is validated as any DAG.
Concrete graph the engine already runs and nested `Fork` cannot express without either false
synchronization or duplicating a node (duplicate side effects):

```text
A, B are roots
C needs [A]
D needs [A, B]
E needs [C]
```

`D` is runnable once A and B finish (not waiting for C/E); `E` once C finishes (not waiting for B).
And the divergence is **observable**: if `B` is `approval: true`, the DAG lets `E` complete and enter
the checkpoint `Completed` set while `B` is interrupted (`#195`: an approval in a parallel group
suspends only its branch); over-joining into a `Fork` makes `E` wait for the gate — so a `Fork`-only
lowering *cannot* meet Phase 1's "identical observable behavior" / concurrency-parity bar.

Also: the **fourth XOR step kind** — `approval` (DESIGN_DOC: a step is exactly one of
`uses`/`agent`/`workflow`/`approval`) — has no execir node to lower to.

**Decision required in Phase 0 (recommended resolution):** extend `execir` with (a) a
`needs:`-preserving construct — a DAG/named-join node, or a graph-of-nodes with per-node dependency
sets and per-branch suspend — so the *authored, reviewable* resource DAG executes faithfully and
`.agent` `parallel { }` becomes a structured special case of it; and (b) an `Approval` node.
Alternatives, stated so they are chosen deliberately, not by omission: *restrict*
`LowerWorkflowResource` to series-parallel graphs and drop the rest from the parity corpus (narrows
what execir can run); or *drop concurrency from the Phase 1 parity bar* (defers the hardest case). A
lowerer that silently serializes a general DAG and still claims DAG parity is not acceptable.

## Phases (one issue each) — order corrected

### Phase 0 — YAML → execir lowering + concurrency/approval model (#256)
Add `lower.LowerWorkflowResource(wf)` so straight-line YAML lowers to an equivalent `Program`. **This
phase also owns the two design decisions above:** the `needs:`-preserving concurrency construct and
the `Approval` node (the fourth XOR step kind). **Library + tests only, no engine change.**
Differential test: a YAML workflow and its `.agent` twin produce identical programs, on whatever
graph class the concurrency decision admits.

### Phase 1 — engine-backed `Invoker` + non-resumable execir run path (#257)
Implement an `execir.Invoker` over the engine's per-step executors, **extending the `Invoker` ABI to
carry structural identity** (prerequisite for Phase 2), bridging execir's scope namespace ↔ the
engine's `ictx`/policy/cost/trace. Run via `Interp` behind a flag, asserting **identical observable
behavior** to the DAG path on **graphs that run to completion** — same traces, output, policy
denials, cost, limits, and **join accuracy** (in the `A,B roots; C[A]; D[A,B]; E[C]` graph, `D` runs
when A and B finish, not when C/E finish). **Suspend/HITL — including the per-branch-suspend case
that motivates the DAG construct — is deliberately *out* of Phase 1's bar and stays DAG-handled; it
is Phase 2's contract.** Proving per-branch suspend on the execir path here would require growing
HITL on the flag path early, which Phase 1 does not do. **Largest PR; the differential harness is
what makes it reviewable.**

### Phase 2 — durable execir: extend the interrupt/nested checkpoint machinery onto the tree (#258)
Not a `{key → result}` replacement of the checkpoint — an **extension** of
`checkpointPayload{Steps, Completed, PendingHitl, Nested}` re-anchored to execir node identities,
plus deterministic replay of pure control flow and memoized completed leaves keyed by structural
identity. Carries the engine's real in-flight states — `PendingHitl` (an incomplete workflow `uses:`
/ `Approval` node paused at a gate) and `Nested` (a suspended subworkflow, `#194`) — **not** an
agent-loop continuation (the inner loop does not suspend). Also adds per-branch suspend for the
concurrency construct. Port the `#105/#106/#192/#195` suites; assert **no duplicate side effects** on
resume via the recording `Invoker`. **The correctness core — highest risk. Sign off the resume model
(all three sub-problems above) before starting.**

### Phase 3 — pin the program (was Phase 4; now a prerequisite for control flow) (#260)
Control-flow `run`/`run --resume` is **impossible until the lowered program is a snapshot artifact**:
production `Invoke`/`Resume` execute `cfg.Graph()` / the graph hydrated from the snapshot (the
*resource* projection with flattened arms), and ADR 001 forbids the runtime from recompiling
`.agent` from source. So: give `ResolvedConfig` an `Executables` field (plumbing `check.Check`'s
**existing** `.agent` `Program.Executables`, and the new YAML `LowerWorkflowResource` output),
persist the serialized `Program` as an `execution_ir` `deployment_artifact`, and populate
`execution_ir_digest` via `WorkflowSpecHashWithExec` (already the production fold, currently called
with `""`). This must hold for a **fresh `Invoke`, not only resume**. Low risk *because* the fold and
construction already exist — the work is persistence + plumbing, not new lowering.

### Phase 4 — control flow end-to-end (was Phase 3) (#259)
Only now is it safe to remove `controlFlowGate`: with the program pinned (Phase 3) and executed
(Phases 1–2), removing the gate routes `.agent` `if`/`for`/`parallel for` through the *program*, not
the flattened DAG. Effect soundness is preserved by construction (`LowerFile` still flattens arms for
`effects.Compute`), so policy/manifest/effect enforcement (#188–#204) is unchanged. Add end-to-end +
resume-mid-loop tests and a control-flow example that `run`s green.

### Phase 5 — retire the DAG runtime (eventual, not filed)
Once execir is at parity, the `WorkflowStep` DAG runtime is redundant. Deleting it is a large,
separate cleanup — deferred.

## Top risks & open questions

1. **Resume grain (#258)** is make-or-break and is *not* naive leaf memoization — it must preserve
   the engine's real in-flight interrupt state, `PendingHitl` (incomplete `uses:` / `Approval` node)
   and `Nested` (`NestedRunState`, `#194`), and extend the `Invoker` ABI with structural identity.
   It must **not** invent an agent-loop continuation — the inner loop does not HITL (exit 5), so an
   interrupted agent step replays wholesale. Sign off all three sub-problems before implementing.
2. **Concurrency model (#256)** must be decided explicitly: `needs:` is a general DAG, not `Fork`.
   Extending execir with a `needs:`-preserving construct + an `Approval` node is the recommended path;
   the alternatives narrow scope deliberately.
3. **Pin before control flow (#260 before #259).** The program must be a hydratable snapshot artifact
   — for fresh `Invoke` too — before the gate is removed, or the DAG runs the flattened arms (the
   exact load error the gate prevents).
4. **Parity is the acceptance bar, not new features.** Phases 1–2 must reproduce the DAG path's exact
   traces/cost/policy/limits/per-branch-suspend; the differential harness is what makes that
   falsifiable.
5. **Two runtimes during transition:** Phases 1–4 mean the DAG and execir paths coexist; a single
   source of truth for "which runs this workflow" avoids drift.

## Suggested order

Phase 0 (#256) is self-contained but must settle the concurrency/approval model first. Settle the
Phase 2 resume grain (#258) before Phase 1's ABI is frozen. Phase 3 (pin, #260) precedes Phase 4
(control flow, #259).
