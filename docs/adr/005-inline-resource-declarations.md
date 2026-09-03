# ADR 005: Inline tool and policy declarations in `.agent`

## Status

Accepted (2026-09-01) — issue [#333](https://github.com/Terfyn/terfyn/issues/333)

## Context

[ADR 003](003-yaml-as-compilation-output.md) decided that **`.agent` is the authoring surface and
YAML is compilation output / interchange**. Its motivating example is a minimal project: four
files and ~60 lines of YAML "dominated by per-resource `apiVersion`/`kind`/`metadata` envelope
overhead," versus ~12 lines in the `.agent` surface.

That decision is only half-delivered. The `.agent` grammar fixed by [ADR 002](002-language-frontend-and-ir-expressiveness.md)
authors **agents and workflows** only. Tools, policies, and schemas still live in YAML/JSON files
that a project must import:

```text
my-agent-app/
  project.yaml          # imports, providers, defaults
  main.agent            # agents + workflow          ← .agent
  tools/workspace.yaml  # tool                        ← YAML envelope
  policies/default.yaml # policy                      ← YAML envelope
  schemas/State.json    # schema                      ← plain JSON
```

For a self-contained small app — the downstream `terfyn-maintainer` PR fixer is 3 agents, 1
workflow, ~3 tools, a couple of policies — the config-vs-language split adds several files and a
layer of indirection for what is conceptually one program. The exact `apiVersion`/`kind`/`metadata`
overhead ADR 003 identified is still paid, just in the files ADR 003 didn't reach.

**The question:** should `.agent` gain declaration blocks for tools and policies (and inline
schemas), so a small project can be a single `.agent` file plus `project.yaml`?

The apparent tension — "this reverses the ADR 003 config/language split" — is a misreading. ADR 003
made `.agent` the authoring surface *for config too*; it simply never extended the grammar there.
This ADR completes ADR 003's stated direction rather than reversing it. The two real concerns are
(1) duplicating the YAML schema surface inside the language, and (2) machine-generated resources and
interchange, which expect YAML.

## Decision

**Extend `.agent` with optional `tool { … }` and `policy { … }` declaration blocks that lower to
the same `ToolResource` / `PolicyResource` the YAML loader produces.** Four qualifications make this
concrete and bound its cost.

### 1. Additive, never mandatory — YAML remains a first-class ingress

> **Superseded by [ADR 007](007-remove-yaml-ingestion.md) (2026-09-03)** on the "YAML remains a
> first-class ingress" point. ADR 007 makes `.agent` the only executable source and moves machine
> ingress to a typed ResourceGraph API; YAML is retained only as output serialization. The rest of this
> ADR — inline `tool`/`policy` (and later environment/provider/workspace) declarations lowering to the
> same resources the YAML loader produced — stands and is in fact what makes ADR 007 possible.

The blocks are one *more* way to author a tool or policy, not a replacement. The YAML loader
(`internal/spec`, `internal/project`) stays exactly as ADR 003 §2 committed: 58 YAML fixtures keep
working, machine-generated resources keep an ingress that is not a parser, and YAML remains the
interchange format. A project may mix — declare its hand-authored tools inline and import a
machine-generated one from YAML — and `terfyn export --format yaml` still materializes the whole
graph. Nothing existing breaks; this is purely a new front-end path onto the **same IR**.

### 2. The blocks lower to the existing resources — no new IR, no second schema

`tool foo { … }` and `policy bar { … }` produce the identical `spec.ToolResource` /
`spec.PolicyResource` the YAML decode produces, merged into the graph by the same
`project.MergeLowered` path the agent/workflow lowering (#197) already uses. There is **one** IR and
**one** validator; the language is a second front end onto it, exactly as ADR 002 framed the
agent/workflow surface. Diagnostics point at `main.agent:12:5` via the `spec.Pos` fields ADR 003 §3
already threaded through the IR.

**"Identical" means every presence-sensitive bit, not just the populated fields.** Some IR semantics
are carried by the *presence* of a source key, not its value, and lowering MUST preserve them — this
is a soundness requirement, not a nicety. The load-bearing case is the closed-world capability
manifest (#204): `ToolSpec.OperationsDeclared` is set from the presence of the YAML `operations` key,
**including an empty `operations: {}`**, and it distinguishes a *closed* manifest (deny every
operation) from an *omitted* one (open, backward-compatible). It participates in resource identity,
survives export/reload, and gates runtime enforcement (the custom YAML marshal exists precisely so
this bit is never lost). Therefore:

```agent
tool foo {
    type native
    operations {}       // MUST lower with OperationsDeclared = true — a closed, deny-all manifest
}
```

An `.agent` tool with an explicit (even empty) `operations {}` block lowers with
`OperationsDeclared = true`, exactly as the YAML `operations:` key does; a tool that omits the block
leaves it false (open). This equivalence is a **required golden/equivalence test**: for each such
presence-sensitive field, a `.agent` declaration and its YAML twin must produce byte-identical
`NormalizedSpecJSON`, so the two front ends can never diverge on a security boundary.

### 3. Duplicate names are an error across every ingress — no precedence

Mixing inline and imported resources (§1) raises the question the ADR must answer normatively: what
happens when a `.agent` `tool github { … }` and an imported `tools/github.yaml` both declare
`Tool/github`, or two `policy default { … }` collide? The answer is the one the language already
enforces for agents and workflows: a duplicate `(kind, metadata.name)` across **any** ingress path —
YAML↔YAML, `.agent`↔`.agent`, or YAML↔`.agent` — is a **load error**. Neither ingress has precedence;
there is no "inline wins" and no "last import wins". Silent shadowing of a `Policy` would be a
disastrous property — a hidden override of the safety surface — so it is forbidden outright.

Concretely, `project.MergeLowered`'s atomic collision check (today over Agents/Workflows via the
`DuplicateResource` error) extends to Tools and Policies, and the cross-ingress duplicate detection
in `project` covers a lowered inline resource colliding with an imported YAML one. The collision is
reported with both source positions so the operator sees exactly which two declarations clash.

### 4. Grammar covers the common surface; uncommon config stays in YAML

To avoid mirroring the entire `ToolSpec` / `PolicySpec` schema into grammar (the real risk), the
blocks express the fields a hand-authored project actually uses:

- **`tool`**: `type` (`native` | `mock` | `mcp` | `http`), `safety { trusted / sideEffects /
  requiresApproval }`, and the closed-world `operations { <op> { effects { … } [schema …] } }`
  manifest (#204). Transport config for `mcp` / `http` (command, url, headers) is included since a
  real project needs it. The native `workspace { root / testCommand }` config (issue #329) is also
  in the common surface — the motivating `implement-review-loop` / `terfyn-maintainer` examples use
  the workspace tool, so leaving it out would immediately push their central tool back to YAML.
- **`policy`**: `preset`, `execution { maxTotalCostUsd / maxWallClockSeconds / … }`,
  `approvals { requiredFor / requireAllTools / permissive }`, `hitl { … }`, and the **full** effect
  permit model — both `permit { … }` and `permitWithApproval { … }`. The latter is not an exotic
  knob: "this effect is allowed autonomously" versus "allowed only behind approval" is one of the
  fundamental distinctions of a bounded-execution policy (it is already first-class in the IR,
  normalization, and diagnostics), so an ordinary guarded policy must be expressible inline rather
  than falling back to YAML.

Anything outside this set — a bespoke `retry`, `security`, provider-specific knobs — is a signal to
author that resource in YAML and import it. The escape hatch (§1) means the grammar never has to be
exhaustive to be useful, and the surface can grow field-by-field as demand appears, each addition
lowering to a field that already exists in the IR.

### 5. Schemas stay files; inline schema literals are explicitly out of scope

Schemas are the one config artifact with **no envelope overhead** — a `schemas/State.json` is a bare
JSON Schema, not an `apiVersion`/`kind`/`metadata` document. Inlining a full JSON Schema grammar into
`.agent` is high-cost (JSON Schema is a large, verbose language) and low-value (it saves no
boilerplate). The existing convention stays: an agent's `output State` / a workflow's `input State`
resolves to `schemas/State.json` (ADR 002). A compact inline object-shape sugar may be revisited
later, but it is **not** part of this decision, and "schemas" is therefore scoped out of the
"single `.agent` file" goal — a small project collapses to `main.agent` + `project.yaml` +
`schemas/*.json`.

### Proposed grammar (illustrative, not final)

```ebnf
Declaration   = AgentDecl | WorkflowDecl | ToolDecl | PolicyDecl ;

ToolDecl      = "tool" Ident "{" { ToolField } "}" ;
ToolField     = "type" Ident
              | "safety"     "{" { SafetyField } "}"
              | "operations" "{" { OperationDecl } "}"    (* an explicit (even empty) block ⇒ closed world *)
              | "workspace"  "{" { WorkspaceField } "}"   (* native: root / testCommand, #329 *)
              | "mcp"        "{" { McpField } "}"          (* when type mcp *)
              | "http"       "{" { HttpField } "}" ;       (* when type http *)
OperationDecl = Ident "{" [ "effects" "{" [ Effects ] "}" ] [ "schema" StringLiteral ] "}" ;
SafetyField   = "trusted" Bool | "sideEffects" Bool | "requiresApproval" Bool ;
WorkspaceField= "root" StringLiteral | "testCommand" StringLiteral ;

PolicyDecl    = "policy" Ident "{" { PolicyField } "}" ;
PolicyField   = "preset"    Ident
              | "execution" "{" { ExecField } "}"
              | "approvals" "{" { ApprovalField } "}"
              | "effects"   "{" [ "permit"             "{" [ Effects ] "}" ]
                                [ "permitWithApproval" "{" [ Effects ] "}" ] "}"
              | "hitl"      "{" { HitlField } "}" ;
```

Parsing note: `tool` is currently only the head of a grant path (`tool.<name>.<op>`) and `policy`
is a contextual agent field (`policy guarded-writes`). Both become contextual keywords at the
**top level** — `tool <Ident> {` / `policy <Ident> {` open a declaration; the existing in-agent and
in-grant uses are unchanged. A top-level `policy Name { … }` *declares* a policy the same file's
agents can then reference by `policy Name`.

### Illustrative before / after

```text
# before: 5 files
project.yaml + main.agent + tools/workspace.yaml + policies/coding.yaml + schemas/State.json
```

```agent
# after: main.agent (+ project.yaml + schemas/State.json)
tool workspace {
    type native
    safety { trusted true; sideEffects true }
    operations {
        read_file  { effects { workspace.read } }
        write_file { effects { workspace.write } }
        run_tests  { effects { process.exec } }
    }
}

policy coding-agent {
    execution { maxTotalCostUsd 5; maxWallClockSeconds 300 }
    effects {
        permit             { workspace.read }
        permitWithApproval { workspace.write; process.exec }
    }
}

agent Implementer {
    model mock/gpt-4
    policy coding-agent
    grants { tool.workspace.read_file; tool.workspace.write_file; tool.workspace.run_tests }
    input State
    output State
}

workflow build(input: State) -> State { … }
```

## Consequences

- **Positive.** The "single `.agent` file" ADR 003 promised for small apps becomes real: one
  authoring language, one mental model, less file-shuffling. The downstream `terfyn-maintainer`
  collapses toward `main.agent` + `project.yaml`. No new IR, no second validator, no drift check.
- **Cost.** A new AST + lowering + printer + checker path for two declaration kinds (lexer needs two
  contextual keywords). Bounded because they lower to existing resources and the grammar is scoped
  (§4). Golden/`fmt` round-trips grow by the new blocks. Two invariants are required implementation
  work, not optional: the presence-preservation equivalence test (§2, `OperationsDeclared` and any
  other source-presence bit) and extending `MergeLowered`'s atomic collision check to Tools/Policies
  (§3).
- **Risk — grammar creep.** Every `ToolSpec`/`PolicySpec` field is a candidate for grammar. Mitigation:
  §4's "common surface + YAML escape hatch" rule and §5's schema exclusion; add fields only on
  demonstrated demand.
- **Neutral.** `project.yaml` (providers, defaults, imports) is unchanged; inlining *it* into
  `.agent` is a separate future question, not part of this ADR.

## Interaction with prior ADRs

- **ADR 002** (language frontend): this extends the surface with two declaration kinds; the "one IR,
  language is a front end" principle is preserved, not amended.
- **ADR 003** (YAML as output): this *completes* ADR 003's authoring-surface decision. YAML-as-ingress
  and `export --format yaml` are explicitly retained (§1).
- **ADR 004** (scope): tools and policies are owned resources at the boundary; authoring them in the
  owned language does not change what Terfyn touches.

## References

- Issue [#333](https://github.com/Terfyn/terfyn/issues/333)
- [ADR 002](002-language-frontend-and-ir-expressiveness.md), [ADR 003](003-yaml-as-compilation-output.md), [ADR 004](004-scope-and-non-goals.md)
- `.agent` grammar: [`docs/LANGUAGE.md`](../LANGUAGE.md); lowering: `internal/lang/lower`, `internal/project`
