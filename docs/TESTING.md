# Fixture workflow tests (`terfyn test`)

Design doc **§10.2** and **§17.4** describe YAML-driven regression tests for workflows. **`terfyn test`** (issue #73) runs those fixtures locally.

## Discovery

- All **`*.yaml`** / **`*.yml`** files under **`<project-root>/tests/`**, recursively.
- If **`tests/`** is missing or no cases run, the command exits **0** and prints that no tests were found.

## Suite file format

Each file targets one workflow **`metadata.name`**:

```yaml
# Optional, for forward compatibility:
# apiVersion: agentic.dev/test/v0

workflow: demo

cases:
  - name: happy-path
    input:
      repo: acme/api
      number: 42
    expect:
      outputContains:
        - summary

  - name: invalid-number
    input:
      repo: acme/api
      number: -1
    expectError: true
```

- **`workflow`**: required; must match a **Workflow** resource in the project.
- **`cases`**: at least one; each **`name`** is required.
- **`input`**: object passed as workflow input JSON (same as **`terfyn run`**).
- **`expect.output`**: for successful runs, each string in **`outputContains`** must appear as a substring in the **JSON-serialized workflow output** (`run.output` in SQLite).
- **`expectError: true`**: the run must **not** succeed (any failure: validation, policy, step error, etc.).

## Capability-assertion suites (issue #332)

A `tests/` file with a top-level **`assert:`** block (instead of `workflow:`) is a **capability-assertion suite**: declarative, model-free invariants over the **effect bound** — the same bound `terfyn plan` prints. They are checked **statically** (no model, no run), so a project's core security guarantees ("the Reviewer can never reach `workspace.write`", "these publish ops are always gated") live next to the agents they constrain and fail `terfyn test` in CI when they drift. This is the guarantee `expect.outputContains` cannot make: driving an agent tool-loop needs a real model, so the "Reviewer can't write" property must be asserted over the plan, not at runtime.

```yaml
name: capability-invariants        # optional; defaults to "capability-assertions"

assert:
  forbidEffect:
    # <root> (an agent or workflow metadata name) must NOT be able to reach <effect>.
    - {agent: Reviewer, effect: workspace.write}
  expectAutonomous:
    # <root> must reach <effect> via an autonomous (agent tool-selection) path.
    - {agent: Implementer, effect: workspace.write}
  expectGated:
    # each tool.<name>.<op> must require approval from every root that can reach it.
    - tool.git.push_branch
    - tool.github.pull_request.post_comment
```

- The root may be written as **`root:`**, **`agent:`**, or **`workflow:`** (aliases).
- **`forbidEffect`** fails if the effect is reachable at all; it names the witnessing `uses`.
- **`expectAutonomous`** fails if the effect is unreachable **or** only reachable via a static `uses:` step (not an autonomous grant).
- **`expectGated`** fails if the op is unreachable, or if any reaching root's policy does not require approval for it (fail-closed tool `safety` counts as gated; a `trusted` tool does not).
- Each assertion is an individual pass/fail row; any violation fails `terfyn test` (exit **1**). A workflow filter (`terfyn test workflow/x`) skips assertion suites — they are project-wide, not workflow-scoped.
- Effect idents are checked against the project's declared effect vocabulary: a `forbidEffect` / `expectAutonomous` naming a **malformed or nonexistent** effect is a **violation** (fail-loud), never a vacuous pass — so a typo can't quietly make a negative guarantee hold. Idents match **hierarchically** (`EffectCovers`, like permit resolution): a forbid on a namespace parent (`workspace`) catches a reachable child (`workspace.write`) and vice versa.

## Execution model

- Same pipeline as **`terfyn run`**: load project, **defaults**, **`-e` / `--env`** overlays, validate graph, then execute each case.
- Each case uses a **fresh temporary SQLite file** (no trace pollution between cases).
- Prefer **`mock`** model providers and **native** / **mock** tools so runs stay **deterministic** without network.

## CLI

```bash
terfyn test
terfyn test workflow/demo
terfyn test demo -o json
```

Global flags: **`--project`**, **`-e` / `--env`**, **`-o table|json|yaml`**.

Non-zero exit if any case fails.

## See also

- **[`EXAMPLES.md`](EXAMPLES.md)** — project and workflow layout.
- **[`examples/regression-test/`](../examples/regression-test/README.md)** — CI gate: `terfyn test` passes on a gated mock publish and fails after dropping `requiredFor` (issue #176). Sample job: [`.github/workflows/terfyn-test.yml`](../.github/workflows/terfyn-test.yml).
- **`internal/testkit/`** — parser and runner.
- **`internal/cli/testdata/wf_tests/`** — minimal example project with **`tests/demo.yaml`**.
