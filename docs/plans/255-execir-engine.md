# Implementation plan — #255: Execute the execution IR (`execir`) on the engine

Status: planned. Epic [#255]. Builds toward the ADR 002 §5 convergence goal.
Depends on #199 (execir + `.agent` lowering, merged) and #207 (deployment snapshot with
the reserved-but-empty `execution_ir_digest`, merged).

## Goal

Make the workflow run path **execute `execir`** ([`internal/execir`](../../internal/execir)) —
first at parity with today's `WorkflowStep` DAG runtime, then to enable `.agent` control flow
(`if`/`for`/`parallel for`) end-to-end, and finally to pin the lowered program into the deployment
snapshot. This closes ADR 002 §5's convergence target: both ingress paths (`.agent` and YAML)
share one interpreter, so execution semantics cannot diverge.

## Where things stand

- **`execir` is a complete, isolated interpreter.** [`execir.go`](../../internal/execir/execir.go)
  defines `Program` (params + a tree of `Node`s: `InvokeTool/Agent/Workflow`, `Let`, `Branch`,
  `Fork`, `Loop`, `Return`); [`interp.go`](../../internal/execir/interp.go) walks it against an
  injected [`Invoker`](../../internal/execir/interp.go). It is deliberately **runtime-independent**:
  no checkpoint/resume, HITL, policy, cost, trace, or telemetry.
- **The engine is a rich DAG runtime.**
  [`execution_dag.go`](../../internal/engine/execution_dag.go) schedules `wf.Spec.Steps` by `needs`
  with bounded concurrency and layers on everything execir lacks — per-step checkpointing, resume,
  HITL suspend (`ErrInterrupted`), policy (`CheckToolCall/Step/Run`), cost/budget, limits, trace +
  OTel.
- **Construction half-exists but is unused on the run path.**
  [`lower.LowerExec`](../../internal/lang/lower/exec.go) + [`check.Check`](../../internal/lang/check/check.go)
  produce `Program.Executables` per workflow; [`plan.WorkflowSpecHashWithExec`](../../internal/plan/workflow_hash.go)
  and [`execir.Program.Digest`](../../internal/execir/digest.go) exist to fold the IR digest into the
  spec hash and the snapshot's `execution_ir_digest`. Nothing production calls any of it, and
  [`project.controlFlowGate`](../../internal/project/agent_sources.go) *refuses* `.agent` `if`/`for`
  workflows at load because there is no engine to run them.

## The crux: two incompatible resume models

- **Engine resume is step-ID/data-based.** A checkpoint is `{ictx.Steps: map[stepID]output,
  totalCost, completed: set[stepID]}`; resume rebuilds the interpolation context and skips completed
  steps ([`loadResumeState`](../../internal/engine/checkpoint.go)). This works only because a DAG is a
  flat set of named steps.
- **`execir` is a nested tree** — branches, sequential/parallel loops, dynamic fan-out, dynamic
  iteration counts. A resume point can be *mid-loop-iteration inside a taken branch*. There is no
  flat "completed step" set that expresses that.

### Decision: replay-with-memoization (Temporal-style)

Give every effectful leaf (`InvokeTool/Agent/Workflow`) a **stable structural key** (node path +
enclosing loop indices). A checkpoint stores `{key → result}` plus cost. Resume **re-runs the
program from the top**; the engine-backed `Invoker` returns the memoized result for any recorded key
and only actually invokes new leaves. HITL suspend = a leaf hitting an approval gate → persist the
memo + `ErrInterrupted`; resume replays memoized results, reaches the gate (now approved), and
proceeds.

This is sound only if non-invoke nodes (`Branch` conditions, `Let`, collection sizes) are **pure and
deterministic** given prior memoized results — which execir already enforces (conditions are pure by
construction, #199). The alternative — a serialized continuation (program counter + scope stack) —
is more complex and brittle across compiler-version changes. **The model must be signed off before
Phase 2 (#258).**

## Phases (one issue each)

### Phase 0 — YAML → execir lowering (#256)
Only `.agent` lowers to execir today. Add `lower.LowerWorkflowResource(wf)` so straight-line YAML
workflows lower to an equivalent flat `Program` (`uses/agent/workflow` → `Invoke*`, interpolation →
`Ref`, `needs`/order → sequential nodes / `Fork`, `output.value` → `Return`). **Library + tests
only, no engine change.** Differential test: a YAML workflow and its `.agent` twin produce identical
programs. **Shippable, low risk.**

### Phase 1 — engine-backed `Invoker` + non-resumable execir run path (#257)
Implement an `execir.Invoker` over the existing per-step executors
([`runToolStep`](../../internal/engine/steps.go), `runAgentStep`, `runSubworkflowStep`), bridging
execir's scope namespace ↔ the engine's `ictx`/policy/cost/trace. Run the workflow via `Interp`
behind a flag/capability, asserting **identical observable behavior** (traces, output, policy
denials, cost, limits) to the DAG path on the straight-line corpus. Resume/HITL stay DAG-handled.
**Largest PR; the differential harness is what makes it reviewable. Highest review surface.**

### Phase 2 — durable execir: memoized replay, checkpoint, resume, HITL (#258)
Add the structural-key memo, its checkpoint payload, and replay-based resume + HITL suspend. Port
the #105/#106/#192/#195 resume/HITL/parallel suites to the execir path. Assert **no duplicate side
effects** on resume via the recording `Invoker`. **The correctness core — highest risk.**

### Phase 3 — control flow end-to-end (#259)
Remove `controlFlowGate`; route `.agent` `if`/`for`/`parallel for` through the execir path. Effect
soundness is preserved by construction (`lower.LowerFile` flattens arms for `effects.Compute`), so
policy/manifest/effect enforcement (#188–#204) is unchanged. Add end-to-end + resume-mid-loop tests
and a control-flow example that `run`s green. **Medium risk, mostly integration.**

### Phase 4 — pin it (#260)
Construct `execir.Program` on the plan/apply/run path, fold `Program.Digest` via
`WorkflowSpecHashWithExec` (already built), populate the snapshot's `execution_ir_digest` (the
reserved-but-empty field from #207), and capture the program as an `execution_ir` artifact so a
pinned resume runs the program it started with. **Low risk — the wiring earlier work was built to
enable.**

### Phase 5 — retire the DAG runtime (eventual, not filed)
Once execir is at parity, the `WorkflowStep` DAG runtime is redundant. Deleting it is a large,
separate cleanup — deferred.

## Top risks & open questions

1. **Resume-model commitment (Phase 2)** is make-or-break. Get sign-off on replay-with-memoization
   before implementing #258.
2. **Parity is the acceptance bar, not new features.** Phases 1–3 must reproduce the DAG path's exact
   traces/cost/policy/limits. The differential-test harness (Phase 0/1) is what makes this
   falsifiable — without it, "execir now runs workflows" is unverifiable.
3. **Non-determinism boundary for replay:** memoization is sound only if `Branch` conditions and
   collection sizes are deterministic given prior memoized results. execir enforces pure conditions
   today; a guard test must lock the invariant.
4. **Dynamic fan-out + resume** (`parallel for` over N items, some completed) is the sharpest memo-key
   case — keys must be index-based and stable.
5. **Two runtimes during transition:** Phases 1–3 mean the DAG and execir paths coexist; a single
   source of truth for "which runs this workflow" avoids drift.

## Suggested order

Phase 0 (#256) is self-contained, unblocks everything, and validates the convergence claim before
touching the engine. Settle the Phase 2 resume model (#258) before starting Phase 1's durability
surface.
