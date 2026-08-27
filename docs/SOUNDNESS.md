# Terfyn Authority Soundness Invariants

> Terfyn exists to make the authority of nondeterministic programs **statically bounded,
> reviewable before execution, and invariant across the execution lifecycle.**

These invariants define the conditions under which Terfyn may claim that **reviewed authority is the
authority available at execution time.** They are not features and not one-time decisions — they are
**permanent correctness obligations**, closer to Rust's `unsafe`-code invariants or a cryptographic
protocol's security assumptions than to an ADR. An ADR records a choice; these record obligations
that every future change must uphold.

Why they need their own document: the frightening class of bug for Terfyn is not a crash. It is a
change that leaves `plan` output looking sound and the test suite green while one mutable dependency
on the execution path silently invalidates the central claim. Every invariant below was *almost*
false in the implementation at some point (see the "Learned from" notes); each is boring exactly
where it is most dangerous.

**Meta-invariant (M).** Any code path that bypasses an invariant below is a **correctness bug**, not
an implementation detail or a performance shortcut. It blocks a release the way a memory-safety bug
does, regardless of whether a test currently catches it.

---

## Invariants

### S1 — A pinned run's authority is authoritative
**Precondition: the run pinned a deployment snapshot** (`runs.deployment_snapshot_digest` non-empty).
For such a run, **every authority-bearing input comes from that snapshot**: current desired state,
current deployed state, source YAML, `.agent` files, and mutable on-disk snapshots
(`.agentic/policy-snapshot.json`, `.agentic/resolved-config.json`) must never widen it. Resume
hydrates graph, policy, capability manifest, and captured schemas from the snapshot — not from
re-resolved current config.
*Enforced in:* `internal/runtime/local` (`prepareForResume`, pinned branch), `internal/engine`
(`Executor.PinnedGraph`), `internal/deploy` (`HydrateGraph`). *Learned from:* #207 initially
hydrated a pinned graph but `CheckToolCall` still compiled policy from the on-disk snapshot `apply`
overwrites — approvals/presets/safety used post-apply authority.

**Bounded carve-out (explicitly outside M).** A run with an **empty** snapshot digest falls back to
current config (`prepareForResume` → `prepareFromConfig`) and therefore *can* be widened by an
intervening apply. This is not a bypass of the invariant; it is a run **outside** its precondition:
- runs created **before #207** have no snapshot to pin (a migration reality, not a hole);
- backends that do not implement `state.ArtifactStore` (in-memory/test stores) retain no artifacts,
  so `pinDeploymentSnapshot` returns an empty digest.

The production SQLite backend pins **every new run**, so S1 holds for all new production runs. The
carve-out is documented (CHANGELOG, `resume_validate.go`) and is the one place S1's "never" is
conditional; a *new* code path that resumes a snapshot-bearing run from current config **is** the M
bug.

### S2 — Discovery never enlarges a *closed* tool's callable set
**Precondition: the tool declares a manifest** (`operations:` present, i.e. `OperationsDeclared` —
including `operations: {}`). For such a **closed-world** tool, remote discovery — MCP `tools/list`,
HTTP discovery, provider metadata — may **assist authoring** (populate a *desired* manifest) but must
**never enlarge the runtime callable set**: an operation absent from the deployed manifest is denied
at dispatch, so a server advertising `delete_repo` after apply cannot make it callable.
*Enforced in:* `internal/tools` (`ApplyMCPSafetyDiscovery` merges only `spec.safety`, never
operations; `CapabilityManifest.Allows`), `internal/policy` (`checkOperationInManifest`).
*Learned from:* #204.

**Closed-world is opt-in — this invariant does not cover an *open* tool.** A tool that omits
`operations:` (every tool in `examples/` today) is an **explicit open world**: `Allows` returns
`true` and `CheckToolCall` does not deny, so its runtime callable set *is* whatever the live server
exposes at dispatch — by author opt-out, not a violation. The honest contract is therefore: *S2
holds iff the tool opted into a manifest.* An engineer who wants the "server cannot grow the callable
set" guarantee must declare `operations:` (see DESIGN_DOC §7.3, ADR 002 — closed-world is opt-in).
`ApplyMCPSafetyDiscovery` not adding operations at *resolve* time is a related but distinct property
from the *dispatch*-time closed world, and does not by itself close an open tool.

### S3 — Closed-empty is distinct from unspecified
An explicitly empty capability set (`operations: {}`) means **closed / deny-all**; an omitted key
means **open / gradual**. This distinction must survive **parsing, normalization, hashing, JSON
serialization, deployment, hydration, YAML export, and resume** — every representation, not just the
in-memory one. A presence bit that `omitempty` can silently drop is a soundness hole. *Enforced in:*
`internal/spec` (`ToolSpec.OperationsDeclared` as `json` identity + `MarshalYAML`), `internal/tools`
(`CapabilityManifest.Closed`). *Learned from:* #204 — the closed-empty world was, in turn,
unrepresentable in `IsClosed()`, then in JSON identity, then in YAML export; each hole silently
reopened the callable universe on shrink-to-empty.

### S4 — Pinned execution performs no mutable authority reads
The pinned execution path must not fall back to current policy files, current schemas, current
manifests, current resource definitions, or current source. The **only** admissible runtime reads
are explicitly modeled inputs whose values are *intentionally* resolved at execution time — e.g.
`env:`-style secret references (persisted verbatim, resolved at request time). Captured schemas must
compile in **isolation**: a fixed opaque resource URL and a loader that cannot open files, so a
same-document `$ref` resolves within the captured bytes and an external `$ref` is a loud error, never
a live disk read. *Enforced in:* `internal/engine` (schema validation routing on `PinnedGraph`),
`internal/schema` (`ValidateContent`). *Learned from:* #253 — `ValidateContent` kept a live
`FileLoader`, so a captured `file://` `$ref` re-read the current file.

### S5 — Semantic identity excludes diagnostic metadata
Source positions, absolute file paths, timestamps, and the local state-database location must **not**
affect deployment identity (spec hashes, snapshot digests, artifact content addresses). Two
serializations of the same semantic program must share a digest; the same program in a different
directory or under a different `--state` path must produce the same snapshot digest. *Enforced in:*
`internal/plan` (`ResolvedGraphDigest`, canonical JSON with `Pos` as `json:"-"`), `internal/deploy`
(`snapshotIdentityV1` excludes paths/timestamps). *Learned from:* #207 — the snapshot digest must
not reuse `ResolvedConfig.Digest()` (which mixes in the absolute state path); positions being
`json:"-"` is load-bearing for content addressing.

### S6 — Every authority widening is observable
If the concrete callable set or the reachable effect set increases, `plan` must expose it —
especially the highest-value line, `autonomous authority: WIDENED`. Authority may never grow
silently between `plan` and `apply`, and manifest drift (an operation appearing, disappearing, or
changing effects/schema) must surface as a state change. *Enforced in:* `internal/plan`
(`attachEffectAuthority`, authority delta), `internal/effects` (`Compute`). *Depends on:* S5 — a
widening that does not change identity cannot be reported. *Scope:* this covers the **declared**
authority in the reviewed config; it does not cover an *open* tool's runtime callable set growing
because the live server added an operation (that widening is invisible to `plan` — the S2 open-world
carve-out — and is another reason to prefer declared `operations:`).

### S7 — Resume cannot duplicate effects
Once durable execir replay lands (#258), a memoized effectful leaf (`InvokeTool/Agent/Workflow`)
must **not be reissued** during replay. Structural identities for effectful operations must be
stable across resume, and replay must be deterministic — a resumed execution must take the same
branch and iterate the same collection it would have without the interruption. Get the keys wrong
and you duplicate a payment / GitHub mutation / deployment; get replay determinism wrong and the
resumed run diverges. *Enforced in (planned):* `internal/execir`, `internal/engine`. *This invariant
must exist before #258 is implemented, not after.*

### S8 — Unknown artifact formats fail closed
Never reinterpret a deployment artifact or snapshot whose `format_version` the runtime does not
understand. An unsupported version fails resume loudly, naming both the found and supported versions.
*Enforced in:* `internal/deploy` (`HydrateGraph`, `ErrUnsupportedFormat`) for the snapshot, resolved
graph, and schema bundle. *Scope:* the `execution_ir` artifact does not exist yet — this invariant
extends to it when #260 lands, and the format check is part of that issue's acceptance criteria.

---

## The Authority TCB

The bugs above show Terfyn has, in effect, a **Trusted Computing Base**: a relatively small set of
code whose correctness determines whether the central product claim is true. The claim flows along
one chain, and anything that determines its arrows is in the TCB:

```text
        SOURCE (.agent / YAML)
              │
              ▼
     checker / effects            (static analyzability)
              │
              ▼
            PLAN                   (authority delta — reviewable)
              │
              ▼
           APPLY                   (snapshot + pins — reproducible)
              │
              ▼
            RUN                    (pinned enforcement)
              │
              ▼
         EXECUTION                 (faithful to reviewed authority)
```

**TCB packages** (raised review standard). The TCB is **whatever can make S1–S8 false** — derived
from the chain above, not a curated favourites list. It must include every package an invariant's
*Enforced in* line names:

```text
internal/lang/check   internal/lang/lower   internal/effects      (SOURCE → checker/effects)
internal/plan (digest/authority)                                  (PLAN)
internal/apply        internal/deploy                             (APPLY / snapshot + pins)
internal/policy       internal/tools        internal/schema       (RUN / pinned enforcement)
internal/runtime      internal/engine       internal/state        (RUN → EXECUTION)
internal/spec (identity, manifest, schema fields)
```

`internal/lang/check` is the static-analyzability pillar (ADR 004), the diagram's first arrow;
`internal/schema` is S4's exhibit (the #253 `FileLoader` near-miss); `internal/apply` is the APPLY
box. Omitting any of them is exactly the "looks complete in a table, does not cover its own
exhibits" failure this document exists to prevent. `internal/execir` joins the list the moment S7
stops being "planned".

**Review standard.** A change to a TCB package is reviewed against the invariants, not just for local
correctness. The reviewer's first question is:

> Which soundness invariants (S1–S8) can this change affect, and where are the regression tests?

A renderer bug or a CLI typo is annoying. A policy path that consults live disk during a pinned
resume means the central security property of the entire system is false while everything looks
green. Those do not deserve the same review standard, and this document exists so they do not get it.

---

## Relationship to ADR 004

[ADR 004](adr/004-scope-and-non-goals.md) governs *what Terfyn is allowed to become* (scope and
non-goals). This document governs *whether the central claim stays true* as it grows. A subsystem
that passes ADR 004's necessity test but cannot be added without weakening an invariant here is not
admissible until the invariant is preserved with tests. Admitting anything that enlarges the TCB
without being necessary to the four pillars (ADR 004 §1) is the most expensive scope creep, because
it grows the surface on which the product claim can silently become false.
