# Implementation plan — #197: Lower the `.agent` AST to the resource model with source maps

Status: in progress. Depends on #196 (AST, merged) and #187 (IR positions, merged).

## Goal

Add a pass that turns the `#196` typed AST into the existing resource model
(`spec.AgentResource` / `spec.WorkflowResource`), plus a source map so any lowered IR
node resolves back to a `.agent` authoring position. This is the **resource projection**
of ADR 002 §5 — the thing `plan`/`apply`/policy/effect analysis run against. It is *not*
the execution lowering (`Branch`/`Loop`/`Fork`/`Join`); that is a sibling projection
arriving with #199 and must be additive, not a refactor of this one.

## Design decisions (and why)

### 1. Sibling projection, not a pipeline
The package lowers **AST → resource projection** directly. It deliberately does **not**
define an intermediate "resource IR" that a later execution lowering would consume:
ADR 002 §5 forbids that, because control flow cannot be recovered from the resource
projection once it exists. #199's execution lowering will read the same AST (the checked
program) independently. `doc.go` records this boundary.

### 2. Identity is structural, never source location (ADR 003, amendment 1)
Generated step IDs derive from **structural position in the program**, never from `Pos`:

| Source construct | Step ID |
|---|---|
| Explicit binding `result = …` | the binding name: `result` |
| Unbound call `github.post_comment(…)` | callee leaf op name, de-duped: `post_comment`, `post_comment_1`, … |
| Hoisted nested call (SSA temp), arg *i* of parent *P* | `P_arg<i>` (recursively `P_arg<i>_arg<k>`) |

The workflow-scoped identity (`PRReview/result`) is conceptual; `WorkflowStep.ID` is
workflow-local, and the validator reserves `/` in step IDs, so temps join path segments
with `_`. The invariant tested directly is **identity ≠ location**: inserting unrelated
lines above a binding, adding comments, or reformatting produces a **byte-identical**
resource graph.

### 3. Reference lowering (`Expr` → interpolation)
A per-workflow environment maps names to interpolation roots:
- workflow param (single param) → `input`: `input.repo` → `${input.repo}`
- binding / parallel branch → its step ID: `result.summary` → `${steps.result.output.summary}`;
  a bare binding `pr` → `${steps.pr.output}` (the trailing `.output` is required — the
  engine's `${steps.<id>.` matcher needs the dot).

### 4. Statements → steps + `needs` (the #192 dependency structure)
Sequential top-level statements chain via `needs` to the previous statement's *frontier*
(order-preserving). `parallel { … }` lowers every branch with the **same** predecessor
frontier as `needs` (concurrent with each other); the block's frontier is *all* branches,
which becomes the `needs` of the next statement (fan-in). This makes every referenced
binding a transitive `needs` ancestor, satisfying the validator's predecessor rule.

- Dotted callee (`github.get_pr`) → **tool** step: `uses: tool.github.get_pr`.
- Single-identifier callee → **workflow** step (`workflow: Util`) when the name is declared
  as a workflow in the same file (a pre-pass classifies every `Decl` before any body is
  lowered, so a call may reference a workflow declared later), or is listed in
  `Options.Workflows` for a workflow in another file; otherwise an **agent** step
  (`agent: Reviewer`). A name declared as both an agent and a workflow in the file is a
  diagnostic. `Options.Workflows` only supplies cross-file names; #198's project-wide symbol
  table replaces it.
- Named args → `with: { name: <lowered> }`. Positional args → `with: { arg0: …, arg1: … }`
  placeholder keys, rebound to real parameter names by #198.
- Nested call arguments SSA-flatten into their own steps (rule 2 temp IDs) referenced by
  `${steps.<temp>.output}`.

### 5. `return <expr>` → `output.value`
`WorkflowOutput.Value = { value: <lowered expr> }`.

### 6. `grants` → `AgentSpec.Tools`
Each `tool.<Name>.<Operation…>` grant reconstructs to a `uses`-form string
(`tool.github.read_pr`) in `AgentSpec.Tools`, with `AgentSpec.ToolsPos` aligned per grant —
an autonomous capability bound, not a call list (ADR 002).

### 7. Types and the effects clause are not forced into the resource model
`.agent` type names (`ReviewRequest`, `Review`, `PullRequest`) are **unresolved type
references**; the resource model's `schema:` fields name *compiled schema files* by path.
Writing a bare type name there would fail schema-file validation, so type refs and the
workflow `effects { }` clause are recorded in the **source map** (position + text) pending
type/effect checking (#193/#198/#190). No new resource-model *spec* field is introduced —
ADR 002 decision 4 is deliberately hostile to new `WorkflowStep`/`WorkflowSpec` fields, and
none proved necessary for the resource projection. (This narrows the issue's speculative
"affected files" list; see PR notes.)

## Source map
`Pos` is already first-class on IR nodes (#187): `Resource.Pos`, `WorkflowStep.Pos`/
`UsesPos`/`AgentPos`/`WorkflowPos`/`NeedsPos`, `AgentSpec.ToolsPos`. Lowering populates
these — that is the primary source map, and it survives normalization / policy compilation
because #187 threads it through. In addition, `SourceMap` is a standalone index from a
**structural key** (`Agent/Reviewer`, `Workflow/PRReview#steps/result`,
`…#grants/tool.github.read_pr`, `…#type/input`) to `Pos`, for passes that hold an IR
identity but not the node, and to record positions that have no dedicated field (type refs,
effects clause).

## Files
- `internal/lang/lower/doc.go` — projection boundary + package contract.
- `internal/lang/lower/lower.go` — `LowerFile(f, opts) (*Result, Diagnostics)`; `Result`
  (`Agents`, `Workflows`, `SourceMap`); `Result.ToGraph()`.
- `internal/lang/lower/sourcemap.go` — `SourceMap` type + keys + lookups.
- `internal/lang/lower/lower_test.go` + `testdata/` — golden, stability, validation,
  position-fidelity tests.
- `internal/project/graph.go` — `MergeLowered` convenience to fold a `Result` into a
  `spec.ProjectGraph` for downstream validate/plan.
- `docs/LANGUAGE.md` — lowering section.
- `CHANGELOG.md` — Unreleased entry.

## Test matrix (issue "Test requirements")
- **Golden**: `adr002.agent` and a nested-call fixture → marshaled YAML compared to
  hand-written equivalents (`GO_UPDATE_GOLDEN=1` to refresh).
- **Stability**: lower `adr002.agent`, then lower a variant with inserted blank lines,
  comments, and reindentation → assert identical resource graph (JSON of the graph is
  byte-identical), proving identity ≠ location.
- **Validation**: assemble a full `ProjectGraph` (lowered PRReview + Reviewer + stub
  agents/tool/policy for the undeclared references) and run
  `spec.ValidateProjectGraph` → no error (ADR 002 fixture lowers to a valid graph).
- **Position fidelity**: a lowered graph with a missing tool grant / missing agent yields a
  `MissingRefError` whose `Pos` points at the `.agent` grant / call-site line, not a
  synthesized name.

## Discovered blocker (Epic F, #188/#204)
`grants` bind several concrete operations on one tool
(`tool.github.read_pr` + `tool.github.read_comments`). `AgentSpec.Tools` as consumed by the
#160 agent loop (`ResolveAgentAdvertisedTools` → `usesByName map[string]string` →
`resolveAgentToolCall`) is architecturally one-operation-per-tool: the model is handed a
ToolDef named after the *tool* and it resolves to a single `uses`. So the ADR 002 fixture
lowers faithfully but does **not** pass full `validateAgentSpecs` until the runtime
advertises operations, not tools. This is exactly the IR-expressiveness gap ADR 002 predicts
("a frontend can only compile to primitives the IR already has"). Lowering is correct and
preserves every grant; lifting the limit is Epic F, tracked by #188/#204. The acceptance
test asserts the lowered graph is clean on everything #197 owns and that this is the *only*
remaining validation error.

## Whole-input reference (follow-up)
The interpolation language addresses workflow input only as `input.<field>`
(`engine.resolvePath` requires ≥2 segments); there is no token for the whole input object,
unlike a step's whole output (`${steps.<id>.output}`). Lowering a bare single-parameter
reference (`return input`, `Util(input)`) would emit `${input}`, which skip-passes validation
(`spec.checkInterpPath` returns nil for <2 segments) and fail-closes at run time. Lowering
therefore **diagnoses** the bare whole-input reference instead of emitting invalid IR. Closing
it properly needs a whole-input token in the engine/validator **and** a subworkflow
input-document mapping (a single-param callee should receive the object as its input document,
not as a one-key `with:` map) — the latter is #194/#198, and #198's `arg0`-rebind does not
compose with the current whole-document convention on its own. Tracked as a follow-up; not the
resource projection's job.

## Out of scope (follow-ups)
Execution lowering / `Branch`/`Loop` (#199); type + effect checking of the effects clause
(#198/#190); multi-operation-per-tool agent advertising (Epic F, #188/#204); a whole-input
interpolation token + subworkflow input-document mapping (#194/#198, above); wiring `.agent`
ingress into the project loader and `agentctl` (Epic H).
</content>
