# ADR 007: `.agent` as the only executable source; YAML is output, not ingress

## Status

**Proposed (draft).** A decision request that would **supersede [ADR 003](003-yaml-as-compilation-output.md)
§2 and [ADR 005](005-inline-resource-declarations.md) §1 on one point** — that the YAML loader "remains
supported ingress to the IR… demoted in docs, not in code." Accepting it reverses that commitment.

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

## Decision (proposed)

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

### Scope: source vs. codec

This decision is about **product architecture, not implementation purity.** The objective is that no YAML
document is *accepted as executable source* — it is **not** "no `yaml.v3` decoder anywhere in the repo."
The YAML marshal/unmarshal codec may remain **private** where `terfyn export` (or isolated compatibility
tooling) needs it; what is removed is `LoadResourceFile` / the project loader as a *graph-construction
frontend for user or resource source*. Tests that specifically exercise the YAML codec may keep YAML
fixtures against that private codec.

## Consequences and migration cost

Large, breaking, and **almost entirely internal** — no human project loses an authoring path (they were
`.agent`-only already), and machine producers gain a better ingress than they had.

- **The export→reload guarantee (ADR 003 §1) is dropped** or re-expressed as export-to-`.agent`. Export
  stays as a one-way YAML/JSON serialization. This removes a contract, not a user capability.
- **~104 resource-YAML + ~25 `project.yaml` fixtures** move off the project loader: convert to `.agent`
  (mechanical; most already have a `.agent` twin pattern), or, for tests that specifically assert YAML
  decode / `spec.Pos` stamping, retarget them at the private codec directly.
- **Machine-ingress consumers gain a typed ingress** (the replacement is a deliverable, step 3 below),
  so ADR 003 §2's second reason is *satisfied*, not ignored — no interface is deleted without a
  replacement.
- **`raise` / `migrate` must be lossless** for any remaining YAML users; the current `raise` field gaps
  (`tool.retry`, `tool.permissions`, `policy.security`, per-operation `schema`) are closed as part of
  the sequence.
- `internal/spec` keeps `yaml.v3` for export, so this is not a dependency removal; the benefit is
  architectural (one executable source language, a clean machine front door), not a smaller build.

## Migration sequence

1. **Immediately:** `LoadProject` accepts only `.agent`; a `project.yaml` at the root is a hard error
   with a `terfyn migrate --to-agent` hint. (This alone enforces the invariant for the user-facing path
   and is the reversible first step.)
2. Migrate the ~25 `project.yaml` fixtures to `.agent`.
3. Introduce / identify a **first-class machine-facing ResourceGraph ingestion API** (the non-parser
   ingress machine producers use instead of YAML source).
4. Move IR-level tests off the project-YAML loader; tests that specifically test the YAML codec keep
   YAML fixtures against the private codec.
5. Close the `raise` field gaps so migration is lossless.
6. Remove `LoadResourceFile` as a normal graph-construction frontend.
7. Retain YAML marshal/unmarshal privately only where export (or isolated compatibility tooling) needs
   it.

## Alternatives

- **A. Status quo (do nothing).** Keep ADR 003 §2 / 005 §1. YAML stays a supported executable ingress
  forever. Rejected as an **end state**: it leaves `.agent` plus a permanent second source-language
  frontend — the "zombie YAML frontend" this ADR exists to retire — even though nothing authors it.
- **B. Reject YAML *projects* at the root only** (keep the resource codec as an ingress for tests /
  machine producers / round-trip). This is **step 1 of the sequence, not an end state.** It enforces
  "no YAML project source" cheaply and reversibly, but still treats resource YAML as an executable
  ingress. Good as a first landing; insufficient as the destination.
- **C. Single executable source (this Decision).** `.agent` is the only executable source; machine
  producers use a typed ingress; YAML is output-only. The clean end state.

## Recommendation

**Accept C as the target architecture, conditioned on the typed machine-ingress replacement (step 3)
being part of the decision — not a follow-up afterthought.** The earlier "keep YAML" recommendation
over-weighted two arguments that do not require a second *executable* frontend: machine producers are
better served by a typed resource-graph ingress than by YAML source, and export can be one-way. A/B are
not acceptable as the destination; B is the sensible first step *toward* C.

Land the enforcement incrementally (sequence above): step 1 immediately (it is reversible and delivers
the user-facing invariant), then 2–7 as the machine ingress and fixture migration are built out, with
step 6 (removing `LoadResourceFile` as a source frontend) gated on step 3 (the typed ingress exists) so
no machine interface is removed before its replacement ships.

## What gates full acceptance

1. Agreement that the ADR-003 §1 export round-trip guarantee is dropped (export becomes one-way).
2. A concrete design for the typed machine-ingress API (step 3) — the non-parser ingress that replaces
   YAML source for machine producers.
3. A fixture-migration plan and owner for the ~104 resource-YAML + ~25 `project.yaml` files (convert vs.
   retarget at the private codec).
4. Closing the `raise` field gaps so `terfyn migrate --to-agent` is lossless.
