# ADR 003: YAML as compilation output, not the authoring surface

## Status

Accepted (2026-08-17)

## Context

[ADR 002](002-language-frontend-and-ir-expressiveness.md) commits to a `.agent` language
frontend as the eventual implementation of the computational subset of `DESIGN_DOC.md` §7.4.
That leaves an unanswered question with large downstream consequences:

**Once `.agent` exists, does YAML remain a first-class authoring surface, or does it become
compilation output?**

The common framing — "keep both, so simple projects never pay the cost of a language" — assumes
YAML is cheaper at small scale. It is not. A minimal project in
[`examples/example1`](../../examples/example1) is four files and roughly 60 lines, dominated by
per-resource `apiVersion`/`kind`/`metadata` envelope overhead. The equivalent in the ADR 002
target surface is about 12 lines. The usual "DSLs only pay off for complex cases" tradeoff does
not apply, because YAML's fixed per-resource overhead is exactly what dominates the simple case.

Keeping two first-class authoring surfaces has real cost: two loaders to keep semantically
identical, two diagnostic paths, doubled golden-test surface, and a `plan`/`apply` UX that must
decide whether it reports source diffs or resource diffs.

## Decision

**YAML becomes compilation output and interchange format. `.agent` becomes the authoring
surface.** Three qualifications make this concrete.

### 1. Generated YAML is not committed, and is not materialized by default

"Compilation output" and "written to disk in git" are separable. Committing generated YAML
gives the worst of both: contributors hand-edit it, CI needs a drift check, merge conflicts land
on generated files, and every pull request shows each change twice — once in `.agent` and once
as YAML noise reviewers learn to skip. That last effect directly damages the reviewable-diff
thesis in `DESIGN_DOC.md` §3.5.

Auditability does not require it. The trustworthy record is applied deployment state in SQLite
plus the hash-linked chain described in [`docs/AUDIT_CHAIN.md`](../AUDIT_CHAIN.md) — not a text
file that anyone could have edited after the fact.

- Default path: `.agent` → in-memory resource graph → plan / apply. No YAML materializes.
- `agentctl export --format yaml` produces it on demand, for inspection or handoff to other
  tools.

### 2. The YAML loader remains supported ingress to the IR — demoted in docs, not in code

`internal/spec` and `internal/project` continue to accept YAML as a valid way to construct a
resource graph. Three reasons:

- **58 YAML fixtures** exist across `internal/cli/testdata`, `test/integration`, and
  `examples/`. IR-level tests *should* be written against the IR; rewriting them into a language
  that does not exist yet is churn.
- Machine-generated resources — a future inspector UI, a module registry, another tool emitting
  Terfyn — need an ingress that is not a parser.
- It is the interchange format.

This is the LLVM model: `.ll` is hand-writable, nobody hand-writes it, it remains fully
supported and the ecosystem depends on its existing. The change is to documentation and
positioning, not to loader capability.

### 3. Source positions become first-class IR data — and this is a prerequisite, not part of the language work

`spec.Pos` (`File`, `Line`, `Column`) is first-class IR metadata on `Resource[T]`, `WorkflowStep`,
and tool/agent references (`AgentSpec.ToolsPos`, `WorkflowStep.AgentPos` / `UsesPos`). Decode
stamps `yaml.Node` Line/Column; syntax errors with no Node stay Path-only. The former
`yamlLineHint` regexp scrape of yaml.v3 error text is gone.

Once `.agent` is the authoring surface, every diagnostic must point at `reviewer.agent:12:5`,
including diagnostics produced deep in the policy engine. An effect-bound violation
(ADR 002, Epic F) has to underline the offending call site. That requires positions as
**first-class fields on IR nodes**, threaded through the full pipeline, independent of any
particular parser.

Doing this while the IR is a handful of structs in
[`internal/spec/kinds.go`](../../internal/spec/kinds.go) is cheap. Doing it after Epics A–G have
added fields and passes is a wide refactor. It is therefore sequenced **first**, ahead of the
effect system.

**Positions are diagnostic metadata only — never identity.** They must not participate in
fingerprints, digests, workflow hashes, or generated resource names. IR Pos fields use
`json:"-" yaml:"-"` so `canonicalResourceJSON` / SpecHash / `ResolvedGraphDigest` ignore them.
Deriving a lowered step's identity from its source coordinates would make an unrelated edit
ten lines above it produce a delete-and-recreate pair in `plan`, for a workflow that did not
semantically change. Identity comes from structural position in the program (enclosing workflow,
binding name, AST child path); location comes from `Pos`. The two must stay separable.

## Consequences

- **Positive:** One authoring surface to document, teach, and produce good diagnostics for.
- **Positive:** Canonicalization moves under project control instead of depending on YAML
  serialization details — which simplifies `internal/plan/fingerprint.go`, `digest.go`, and
  `workflow_hash.go`.
- **Positive:** No generated artifacts in git, so pull-request diffs stay one-to-one with intent.
- **Negative:** YAML-specific diagnostic investment stops transferring.
  [`internal/spec/yamlhint.go`](../../internal/spec/yamlhint.go) and
  [`internal/project/yamlpaths.go`](../../internal/project/yamlpaths.go) should be maintained to
  preserve current behavior but not extended.
- **Negative:** `agentctl fmt` currently formats YAML. It eventually formats `.agent`. Further
  investment in the YAML formatter is not worthwhile.
- **Negative:** `agentctl init` scaffolds YAML today and must eventually scaffold `.agent`.
  Until Epic H ships, it keeps scaffolding YAML.
- **Negative:** Existing users and all four `examples/` projects are YAML. Because the loader
  stays supported (decision 2), no migration is forced — but README and docs must be re-led on
  `.agent` when Epic H lands, and examples should be ported to demonstrate the intended surface.
- **Reversal cost:** Low. Decisions 2 and 3 are valuable regardless; only decision 1 and the
  documentation re-lead are specific to this choice.

## Interaction with ADR 002

This ADR does not change ADR 002's ordering. Source positions (decision 3) are sequenced ahead
of the effect system; everything else here takes effect when Epic H ships. Until then YAML
remains, in practice, the only authoring surface.

## References

- ADR 002 — language frontend and IR expressiveness
- ADR 001 — control plane vs runtime boundary
- `docs/DESIGN_DOC.md` §3.5 (reviewable changes), §7 (YAML spec v0), §10 (`agentctl fmt`, `init`)
- `docs/AUDIT_CHAIN.md` — tamper-evident applied state
