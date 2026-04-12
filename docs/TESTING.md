# Fixture workflow tests (`agentctl test`)

Design doc **§10.2** and **§17.4** describe YAML-driven regression tests for workflows. **`agentctl test`** (issue #73) runs those fixtures locally.

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
- **`input`**: object passed as workflow input JSON (same as **`agentctl run`**).
- **`expect.output`**: for successful runs, each string in **`outputContains`** must appear as a substring in the **JSON-serialized workflow output** (`run.output` in SQLite).
- **`expectError: true`**: the run must **not** succeed (any failure: validation, policy, step error, etc.).

## Execution model

- Same pipeline as **`agentctl run`**: load project, **defaults**, **`-e` / `--env`** overlays, validate graph, then execute each case.
- Each case uses a **fresh temporary SQLite file** (no trace pollution between cases).
- Prefer **`mock`** model providers and **native** / **mock** tools so runs stay **deterministic** without network.

## CLI

```bash
agentctl test
agentctl test workflow/demo
agentctl test demo -o json
```

Global flags: **`--project`**, **`-e` / `--env`**, **`-o table|json|yaml`**.

Non-zero exit if any case fails.

## See also

- **[`EXAMPLES.md`](EXAMPLES.md)** — project and workflow layout.
- **`internal/testkit/`** — parser and runner.
- **`internal/cli/testdata/wf_tests/`** — minimal example project with **`tests/demo.yaml`**.
