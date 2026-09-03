# ADR 007: `.agent` as the only executable source; YAML is output, not ingress

## Status

**Accepted (2026-09-03).** This ADR **supersedes [ADR 003](003-yaml-as-compilation-output.md) §2 and
[ADR 005](005-inline-resource-declarations.md) §1 on one point** — that the YAML loader "remains supported
ingress to the IR… demoted in docs, not in code." Those ADRs kept YAML as an executable ingress; this one
reverses that: `.agent` becomes the *only* executable source, and machine producers get a typed
resource-graph ingress instead of a second source-language frontend. The rest of ADR 003 (YAML as
compilation output / interchange, `.agent` as authoring surface) and ADR 005 (inline resource
declarations) stand. Implementation is sequenced (see "Migration sequence"); the hard removal of YAML
ingress is gated on the replacement paths shipping first.

An [earlier draft](https://github.com/Terfyn/terfyn/pull/465) of this ADR recommended *against* removal,
on the grounds that machine producers need a non-parser ingress and that `terfyn export` round-trips
through the loader. That recommendation has been revised: neither argument justifies a permanent second
*executable* frontend. The replacement for the machine ingress is not "machines emit `.agent` text" — it
is a **typed resource-graph ingress**, made a first-class part of this decision — and export can be a
one-way serialization without being compilable source.

## Context

ADR 003 made `.agent` the authoring surface and YAML the compilation-output / interchange format, but
was explicit that **the YAML loader stays in code** (§2), for three reasons: fixture churn,
machine-generated resources needing a non-parser ingress, and interchange (with export round-tripping
back through the loader, §1). ADR 005 §1 reaffirmed it.

Issue #430 then shipped that direction **without touching loader capability**:

- `terfyn init` / `terfyn new` scaffold `.agent`-only; **all 15 `examples/*` are `.agent`-only**.
- The `.agent` grammar now expresses the **entire resource model** — tools (`mcp` / `http` /
  `workspace`), policies (`hitl`), environments, custom `provider` aliases, and multi-field workflow
  outputs via object-literal returns (#440).
- `terfyn migrate --to-agent` (on `internal/lang/raise`) converts a YAML project to `.agent`.
- A deprecation notice fires when a project loads from `project.yaml`.

So the user-facing goal — humans do not author YAML — is **already met by positioning**. This ADR asks a
different, architectural question: *should YAML cease to be an executable ingress into Terfyn at all, or
does it remain a supported second source-language frontend indefinitely?*

The three ADR-003 reasons, re-examined:

1. **Fixture churn** — materially weaker now: there is a `.agent` target for every construct **and** a
   migration tool. Churn is a one-time sequenced cost, not a permanent constraint.
2. **Machine-generated resources** — valid that machine producers need a non-parser ingress, but *that
   ingress does not have to be YAML project source.* A typed resource-graph / API ingress serves it
   better and does not make YAML a second executable source language. This is the crux, and it is
   promoted to a **deliverable** of this decision rather than a reason to keep YAML.
3. **Interchange / round-trip** — `terfyn export --format yaml` producing YAML does **not** imply
   `terfyn run project.yaml`. Export can be a one-way inspection / handoff / diagnostics format, as in
   many systems. The ADR-003 §1 "export reloads to the same graph" guarantee is a choice, not a
   necessity; it is dropped (or re-expressed as export-to-`.agent`).

Current YAML the loader ingests (measured): **104** resource files (`kind:` documents) and **25**
`project.yaml` / `project.yml` fixtures under `internal/` and `test/`; plus the `export → WriteProjectDir
→ reload` round-trip test. Not affected: `terfyn test` files (`tests/*.yaml`, decoded by
`internal/testkit`, not the resource codec) and JSON Schemas.

## Decision

Adopt a single-executable-source architecture with separate human and machine front doors:

```
 Human source                          Machine producer
      │                                      │
      ▼                                      ▼
   .agent                        typed API / canonical machine
      │                            serialization (ResourceGraph)
      ▼                                      │
 parser / compiler ─────────────────────────┤
      │                                      ▼
      └────────────────►  Canonical typed IR / ResourceGraph
                                 │
                                 ├──► validate / plan / apply / run
                                 └──► export YAML / JSON  (one-way output)
```

- **`.agent` is the canonical human source language** — the only executable *source* into the IR.
- **The typed ResourceGraph / a stable API is the canonical machine ingress** — machine producers build
  the IR directly (or via a canonical machine serialization), not through a second source-language
  parser.
- **YAML is an optional output serialization** — `terfyn export` emits it for inspection and handoff;
  it is not project or resource source.

### The invariant this ADR establishes

> A Terfyn project has exactly one executable source language: `.agent`. No YAML document may be supplied
> to `validate`, `plan`, `apply`, or `run` as project or resource source. Machine integrations operate on
> the typed resource graph / API rather than through a second source-language frontend. YAML may remain an
> output serialization format.

The strongest one-line formulation: **Terfyn has one language, one IR, and two front doors — `.agent` for
humans and a typed ResourceGraph API for machines. Serialization formats are not executable source
languages.**

### The machine ingress is not a safety bypass

The two front doors **converge on the same canonical ResourceGraph and share one control plane.** The
typed machine ingress is an alternative *serialization / construction* boundary — never an authority or
validation bypass. It constructs the same `ResourceGraph` the `.agent` compiler produces and MUST pass
through the identical downstream pipeline: normalization, schema and reference validation, policy lint,
effect / capability analysis, spec fingerprinting, and deployment validation. There is no privileged
machine fast path around the control plane; a graph built via the API is held to exactly the same
soundness checks as one compiled from `.agent`. (This is essential given Terfyn's premise: the safety
guarantees live in the IR pipeline, so any ingress that reached `plan`/`apply`/`run` without them would
be a hole, regardless of format.)

```
 .agent ── parser / compiler ──┐
                               │
 machine typed API ────────────┤
                               ▼
                   canonical ResourceGraph
                               │
                     normalize / validate      (schema + reference resolution)
                               │
                        policy lint
                               │
                effect / capability analysis
                               │
                    spec fingerprint
                               │
                    plan  →  apply  →  run
```

### Scope: source vs. codec

This decision is about **product architecture, not implementation purity.** The objective is that no YAML
document is *accepted as executable source* — it is **not** "no `yaml.v3` decoder anywhere in the repo."
The YAML marshal/unmarshal codec may remain **private** where `terfyn export` (or isolated compatibility
tooling) needs it; what is removed is `LoadResourceFile` / the project loader as a *graph-construction
frontend for user or resource source*. Tests that specifically exercise the YAML codec may keep YAML
fixtures against that private codec.

## Consequences and migration cost

Large and **deliberately breaking.** The migration is mostly internal in *implementation* cost, but it
intentionally removes the **supported YAML execution path** for legacy projects and machine producers —
ADR 003 explicitly kept YAML as supported ingress, and such projects execute from YAML today even though
authoring it is deprecated. Those consumers must have migration / replacement paths **before** their
ingress is removed (this is why the sequence below gates the hard removal on lossless migration and on the
typed machine ingress shipping first). No *human authoring* path is lost — humans were `.agent`-only
already — but a real, supported *execution* path is being withdrawn, and this ADR treats that as a
compatibility break, not cleanup.

- **The export→reload guarantee (ADR 003 §1) is dropped** or re-expressed as export-to-`.agent`. Export
  stays as a one-way YAML/JSON serialization. This removes a contract, not a user capability.
- **~104 resource-YAML + ~25 `project.yaml` fixtures** move off the project loader: convert to `.agent`
  (mechanical; most already have a `.agent` twin pattern), or, for tests that specifically assert YAML
  decode / `spec.Pos` stamping, retarget them at the private codec directly.
- **Machine-ingress consumers gain a typed ingress** (the replacement is a deliverable, step 4 below,
  and step 6's removal is gated on it), so ADR 003 §2's second reason is *satisfied*, not ignored — no
  interface is deleted without a replacement.
- **`raise` / `migrate` must be lossless** for any remaining YAML users; the current `raise` field gaps
  (`tool.retry`, `tool.permissions`, `policy.security`, per-operation `schema`) are closed as part of
  the sequence.
- `internal/spec` keeps `yaml.v3` for export, so this is not a dependency removal; the benefit is
  architectural (one executable source language, a clean machine front door), not a smaller build.

## Migration sequence

The hard break (rejecting YAML) is **gated on the replacement being real**: a user told "`project.yaml`
is no longer accepted; run `terfyn migrate --to-agent`" must actually be able to migrate a valid legacy
project losslessly, and a machine producer must have the typed ingress before its YAML path is removed.
So gap-closing comes first, not last.

1. **Close the `raise` / `migrate` field gaps and prove YAML → `.agent` migration is lossless** — cover
   the constructs `raise` currently refuses (`tool.retry`, `tool.permissions`, `policy.security`,
   per-operation `schema`), with a round-trip test that every accepted YAML resource migrates to `.agent`
   and re-lowers to the same graph. *Nothing user-facing is rejected until this holds.*
2. Make `LoadProject` reject a root `project.yaml` with a `terfyn migrate --to-agent` hint (the reversible
   first user-facing enforcement — Alternative B — now safe because step 1 guarantees a working exit).
3. Migrate the ~25 `project.yaml` fixtures to `.agent`.
4. **Ship the first-class typed ResourceGraph machine ingress** (the non-parser path machine producers
   use instead of YAML source), routed through the same normalization/validation/effect pipeline (see
   "The machine ingress is not a safety bypass").
5. Move IR-level tests off the project-YAML loader; tests that specifically test the YAML codec keep YAML
   fixtures against the private codec.
6. Remove `LoadResourceFile` as a graph-construction source frontend — **gated on step 4** so no machine
   interface is withdrawn before its replacement ships.
7. Restrict the YAML marshal/unmarshal codec to export / isolated compatibility internals.

## Alternatives

- **A. Status quo (do nothing).** Keep ADR 003 §2 / 005 §1. YAML stays a supported executable ingress
  forever. Rejected as an **end state**: it leaves `.agent` plus a permanent second source-language
  frontend — the "zombie YAML frontend" this ADR exists to retire — even though nothing authors it.
- **B. Reject YAML *projects* at the root only** (keep the resource codec as an ingress for tests /
  machine producers / round-trip). This is **step 2 of the sequence (after step 1 proves migration is
  lossless), not an end state.** It enforces "no YAML project source" cheaply and reversibly, but still
  treats resource YAML as an executable ingress. Good as a first landing; insufficient as the
  destination.
- **C. Single executable source (this Decision).** `.agent` is the only executable source; machine
  producers use a typed ingress; YAML is output-only. The clean end state.

## Decision rationale

C is adopted as the target architecture, with the typed machine-ingress replacement (step 4) a
first-class **deliverable of the decision**, not a follow-up afterthought. The earlier "keep YAML"
recommendation over-weighted two arguments that do not require a second *executable* frontend: machine
producers are better served by a typed resource-graph ingress than by YAML source, and export can be
one-way. A and B are not acceptable as the destination; B is the sensible first *enforcement* step toward
C. Enforcement lands incrementally per the sequence, with two hard gates so nothing is withdrawn before
its replacement exists.

## Sequencing gates (binding)

These orderings are part of the decision; the hard removal must not outrun its replacements:

1. **Lossless migration precedes rejection.** Step 1 (close `raise`/`migrate` gaps, prove YAML → `.agent`
   is lossless) must land before step 2 (`LoadProject` rejects `project.yaml`) — a rejection that hands
   the user a `migrate` command that cannot migrate their project is not acceptable.
2. **The typed ingress precedes removing `LoadResourceFile`.** Step 4 (ship the typed ResourceGraph
   machine ingress) must land before step 6 (remove YAML resource ingress) — no machine interface is
   deleted without its replacement.
3. **The typed ingress is not a safety bypass** — it routes through the same normalization / validation /
   effect / fingerprint / deployment pipeline as `.agent` (see above).
4. **Export becomes one-way** — the ADR-003 §1 export→reload guarantee is dropped (export remains a
   YAML/JSON output serialization).
