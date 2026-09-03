# ADR 007: Removing YAML as an accepted loader ingress

## Status

**Proposed (draft).** This ADR is a decision request, not an accepted decision. It would **supersede
[ADR 003](003-yaml-as-compilation-output.md) §2 and [ADR 005](005-inline-resource-declarations.md) §1
on one specific point** — that the YAML loader "remains supported ingress to the IR… demoted in docs,
not in code." Accepting it reverses that commitment. It is written so the maintainers can decide with
the full cost in view; the recommendation (below) is *not* to accept full removal now.

## Context

ADR 003 made `.agent` the authoring surface and YAML the compilation-output / interchange format, but
was explicit that **the YAML loader stays in code**:

> ADR 003 §2: "`internal/spec` and `internal/project` continue to accept YAML as a valid way to
> construct a resource graph… This is the LLVM model: `.ll` is hand-writable, nobody hand-writes it, it
> remains fully supported… The change is to documentation and positioning, not to loader capability."

ADR 005 §1 reaffirmed it ("YAML remains a first-class ingress… the YAML loader stays exactly as ADR
003 §2 committed"). ADR 003 gave **three reasons** to keep the loader:

1. **Fixture churn** — many IR-level tests are written against YAML; rewriting them into a language
   that did not yet exist would be churn.
2. **Machine-generated resources** — a future inspector UI, a module registry, or another tool emitting
   Terfyn needs an ingress that is *not* a parser.
3. **Interchange** — YAML is the interchange format, and `terfyn export` output must round-trip back
   through the loader to the same graph (ADR 003 §1).

Since those ADRs, issue #430 shipped the intended direction **without** touching loader capability:

- `terfyn init` / `terfyn new` scaffold `.agent`-only; **all 15 `examples/*` are `.agent`-only**.
- The `.agent` grammar now expresses the **entire resource model** — tools (incl. `mcp` / `http` /
  `workspace`), policies (incl. `hitl`), environments, custom `provider` aliases, and multi-field
  workflow outputs via object-literal returns (#440).
- `terfyn migrate --to-agent` (built on `internal/lang/raise`) converts a YAML project to `.agent`.
- A deprecation notice fires when a project is loaded from `project.yaml`.
- ADR 003's Phase-3 "positioning" is done: the docs present `.agent` as the sole authoring surface and
  YAML as non-authoring interchange only.

So reason (1) is materially weaker — there is now a `.agent` target for every construct **and** a
migration tool. Reasons (2) and (3) are unchanged and remain architecturally valid. The question this
ADR forces: now that nothing *needs* to be authored in YAML, should the loader stop *accepting* it?

Current YAML that the loader ingests (measured):

- **104** YAML resource files (`kind:` documents) under `internal/` and `test/`.
- **25** `project.yaml` / `project.yml` fixtures under `*/testdata`.
- The `terfyn export` → `WriteProjectDir` → **reload** round-trip (`internal/project/export_test.go`,
  `tool_manifest_roundtrip_test.go`).

Not affected by this ADR: `terfyn test` files (`tests/*.yaml`) use their own decoder in
`internal/testkit`, not the resource codec, and JSON Schemas are JSON. Both stay regardless.

## Decision (proposed)

Remove YAML as an **accepted source ingress**: `internal/spec.LoadResourceFile` and the
`internal/project` loader no longer construct a resource graph from `project.yaml` or imported YAML
resource files. The YAML **codec** (`gopkg.in/yaml.v3` marshalling in `internal/spec`) is retained
**only** where `terfyn export --format yaml` needs it to *emit* YAML; it is no longer a loading path.

Concretely:

- `LoadProject` / `loadYAMLGraph` are deleted or reduced to a hard error directing the user to
  `terfyn migrate --to-agent`.
- The project loader accepts `.agent` sources only; a `project.yaml` at the root is a load error.
- Export still produces YAML for inspection/handoff, but the "round-trips back through the loader"
  guarantee (ADR 003 §1) is either dropped or re-expressed as "exports to `.agent`."

## Consequences and migration cost (honest accounting)

This is a **large, breaking, mostly-internal** change. What it costs:

- **The export→reload round-trip contract breaks.** ADR 003 §1 promises `export --output DIR` writes a
  project that reloads to the same graph. With YAML ingestion gone, that no longer holds unless export
  is changed to emit `.agent` (which requires a **public, complete `.agent` serializer** — today the
  printer prints the AST, and `raise` reconstructs an AST from a graph, but there is no supported
  "graph → `.agent` file" product surface, and `raise` deliberately refuses a few fields).
- **~104 resource-YAML fixtures + ~25 `project.yaml` fixtures must be migrated or relocated.** Options
  per fixture: convert to `.agent` (mechanical, but some assert YAML-specific decode / `spec.Pos`
  line-column stamping and would need rewriting or deletion), or move behind a codec-only test helper
  that unmarshals YAML without going through the loader. Either way this is a wide, error-prone sweep
  touching `internal/spec`, `internal/project`, `internal/cli/testdata`, `internal/engine/testdata`,
  and `test/integration`.
- **Machine-ingress consumers lose a non-parser ingress** (ADR 003 §2 reason 2). Any tool that emits
  Terfyn resources (an inspector UI, a module registry, codegen) must now emit `.agent` text or call a
  Go API directly — a parser becomes mandatory where a data format used to suffice. This is the most
  consequential *architectural* loss and the hardest to reverse.
- **`raise` / `migrate` become load-bearing** for anyone with legacy YAML, and must cover every field
  losslessly (today `raise` refuses a small set, e.g. `tool.retry`, `tool.permissions`,
  `policy.security`, per-operation `schema`) — those gaps would need closing first.
- The `spec` package keeps the `yaml.v3` dependency regardless (for export), so this does **not** remove
  a dependency — the benefit is consistency ("one ingress"), not a smaller build.

## Alternatives

- **A. Status quo (do nothing) — recommended default.** Keep ADR 003 §2 / 005 §1 as written. `.agent`
  is already the sole *authoring* surface (docs + tooling, Phase 3), and YAML stays a supported
  non-authoring ingress for interchange, machine producers, and fixtures. Zero cost, no reversal, no
  broken round-trip. The user-facing goal of #430 is already met.
- **B. Scope removal to the project root only.** Reject a hand-authored `project.yaml` **project** at
  the loader (a real "no YAML *projects*" stance), but keep resource-YAML parsing (`LoadResourceFile`)
  for the export round-trip, machine ingress, and IR-level fixtures. This removes YAML *authoring* in
  code (not just docs) with a far smaller blast radius, and is reversible. It does not deliver "one
  ingress," but it does make "no YAML project source" enforced rather than merely deprecated.
- **C. Full removal (this ADR's Decision).** Everything above. Largest cost; reverses two accepted
  ADRs on the loader point; needs the export contract and machine-ingress story resolved first.

## Recommendation

**Do not accept full removal (Alternative C) now.** The two durable reasons ADR 003 §2 gave for keeping
the loader — machine-generated resources and the interchange round-trip — remain valid, and #430
already achieved its user-facing goal (no one authors YAML) through positioning plus a complete
`.agent` grammar. Full removal trades a large internal migration and a real loss of non-parser ingress
for a mostly-aesthetic "single ingress" consistency.

If enforcing "no YAML *project source* in code" (beyond the current deprecation warning) is desired,
prefer **Alternative B**: a bounded, reversible step that rejects YAML *projects* while retaining the
resource codec for tests, export round-trip, and machine ingress.

## What would gate accepting C

1. A concrete driver beyond consistency — e.g. a maintenance or soundness cost of the dual ingress that
   status-quo docs cannot address.
2. A decision on the ADR 003 §1 export contract: drop the reload guarantee, or ship a supported
   `graph → .agent` serializer (and close the `raise` field gaps) so export emits `.agent`.
3. A migration plan for the ~104 resource-YAML + ~25 `project.yaml` fixtures (convert vs. codec-only
   helper), with an owner and a landing sequence.
4. A machine-ingress answer: the supported way for non-Terfyn producers to emit resources without a
   parser, or an explicit decision to drop that capability.
