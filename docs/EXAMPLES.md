# Examples

Short, runnable **`.agent`** patterns for the **`agentic.dev/v0`** resource model. For the full spec, CLI behaviour, and field semantics, see [**`DESIGN_DOC.md`**](DESIGN_DOC.md); for the grammar, [**`LANGUAGE.md`**](LANGUAGE.md); for **`terfyn test`** fixture format, [**`TESTING.md`**](TESTING.md).

Under [**ADR 007**](adr/007-remove-yaml-ingestion.md), **`.agent` is the only executable source** — every example below is authored in `.agent`, and every `examples/*` project is `.agent`-only. YAML appears here only where the compiled/interchange form is worth showing.

A checked-in **OpenAI `support_snippet`** project (**section 4**) lives under [**`examples/example1/`**](../examples/example1/); its project name is the directory (`example1`). From the repository root, pass **`--project examples/example1`** to **`terfyn`** (or **`cd` there** and use **`--project .`**).

### Formatting `.agent` sources (`terfyn fmt`)

**`terfyn fmt`** formats every **`.agent`** source under the project root to canonical form (4-space indent, normalized spacing) and normalizes any YAML still in the project closure. **`--check`** exits **1** if any file would change (CI). **`.agent` comments are not preserved** on rewrite — commit or branch before running.

```bash
terfyn fmt --project my-agent-system
terfyn fmt --check --project .
```

---

## 1. Scaffold with `terfyn init`

```bash
terfyn init my-agent-system
```

Creates a single-file `.agent` project — **no `project.yaml`, no YAML resources**:

```text
my-agent-system/
  main.agent            # a starter agent, a `default` policy, and the hello workflow
```

A Terfyn project is authored **entirely in `.agent`** (ADR 002 / [ADR 007](adr/007-remove-yaml-ingestion.md); [grammar reference](LANGUAGE.md)): agents, workflows, tools, and policies are all `.agent` declarations, discovered and compiled automatically from any `.agent` file under the project root. The project name is the directory name and built-in model providers need no configuration, so nothing else is required.

> **YAML is no longer a project source.** Under [ADR 007](adr/007-remove-yaml-ingestion.md) `.agent` is the only executable source: a `project.yaml` handed to `validate`/`plan`/`apply`/`run` is **refused** with a `terfyn migrate --to-agent` hint. `terfyn export --format yaml` still emits YAML, but as a **one-way** interchange output that is never re-loaded as source; machine producers build the graph through the typed **ResourceGraph** ingress instead of a second source language. To convert a legacy YAML project, run `terfyn migrate --to-agent` (it raises declaratives **and** workflows).

---

## 2. Project config: `defaults` and `limits` (`.agent`)

A `.agent` project needs **no `Project` declaration** — the project name is the directory and built-in model providers resolve with no configuration. Two optional top-level singletons tune project-wide behaviour (declare each **at most once**, in any `.agent` file under the root).

`defaults` sets fallbacks any agent/workflow inherits when it omits the field. `defaults.model` is **`namespace/model_id`** (a built-in namespace like `mock`/`openai`, or a `provider` alias). Optional **`defaults.runtime`** sets where a workflow runs: the built-in **`local`** engine (or omit for implicit local), or an external agent runtime such as **`claude-code`** (see [`EXTERNAL_RUNTIME.md`](EXTERNAL_RUNTIME.md) and section 9); a workflow that omits `runtime` inherits it.

```
defaults {
    policy default
    model mock/gpt-4
    runtime local
}

limits {
    maxToolInputBytes 262144       // 256 KiB; truncate by default
    maxToolOutputBytes 262144      // 256 KiB
    maxCheckpointBytes 1048576     // 1 MiB; fail closed (never truncate checkpoints)
    toolInputExceedPolicy truncate
    toolOutputExceedPolicy truncate
    checkpointExceedPolicy fail
}
```

`limits` is the project-wide execution-limit baseline; a Tool's own `limits { … }` block overrides individual fields at top precedence. `maxStateBytes` is an alias for `maxCheckpointBytes`. When `truncate` is set, long string fields are shortened in-place (top-level keys preserved); `fail` aborts the step. **`checkpointExceedPolicy` must be `fail`** — truncating durable checkpoint state is rejected by `terfyn validate`.

---

## 3. Policy, native tool, and a tool-only workflow (`.agent`)

A policy, a native tool, and a workflow whose single step calls a tool — all `.agent` declarations in any `.agent` file under the project root. A tool call is `<tool>.<operation>(args)`; a workflow step is a binding or the trailing `return`.

```
policy default {
    execution {
        maxWallClockSeconds 300
        maxTotalCostUsd 5
    }
}

tool helper {
    type native                 // built-in operations such as echo (see the design doc for names)
    safety { sideEffects false }
}

workflow hello(input: any) policy default {
    return helper.echo(message: "hello")
}
```

Run the usual loop from the parent of the project directory:

```bash
terfyn validate --project my-agent-system
terfyn plan   --project my-agent-system
terfyn apply  --project my-agent-system --auto-approve
terfyn run    workflow/hello --project my-agent-system
```

---

## 3b. MCP tool over HTTP (streamable HTTP)

For MCP servers exposed over **HTTP** (streamable HTTP transport: one **POST** per JSON-RPC message), give the tool an `mcp` block with **`transport "http"`** and a **`url`** (must be **`http://`** or **`https://`**). Optional **`headers`** are string key/value pairs — use **`env:VAR_NAME`** for a secret rather than an inline literal.

```
tool remote_mcp {
    type mcp
    mcp {
        transport "http"
        url "https://mcp.example.com/v1/mcp"
        headers {
            "Authorization" "env:MCP_BEARER_TOKEN"
        }
    }
}
```

**Security**

- Prefer **HTTPS** in production. The default Go client performs **normal TLS certificate verification** against the system trust store; do not disable verification for MCP calls.
- **stdio** and **http** are mutually exclusive in the `mcp` block: set **`command`** only for stdio, **`url`** only for HTTP (validated at `terfyn validate`).
- Workflow trace events for tool steps record **`uses`** and cost, not HTTP headers or resolved env values; keep custom logging of MCP traffic free of secrets. A literal secret in a header is flagged at apply/run and never resolved to a stored value.

---

## 4. Real OpenAI example (`gpt-4o-mini`)

This is a small but **end-to-end** project: a **native echo** step supplies fixed “policy” text, then **`gpt-4o-mini`** drafts a one-line customer reply. You need a valid **[OpenAI API key](https://platform.openai.com/api-keys)** and outbound **HTTPS** to `api.openai.com`.

**Repo copy:** [**`examples/example1/`**](../examples/example1/) — **`terfyn validate --project examples/example1`** from the repo root, or **`terfyn validate --project .`** after **`cd examples/example1`**.

The runtime calls OpenAI’s **`/v1/chat/completions`** endpoint. The agent **must** answer with a **single JSON object** (no markdown fences); the engine parses that object and exposes its fields to **`spec.output`**.

**`totalCostUsd` on runs** is accumulated from each step’s reported cost. Native tools report **0**. For **OpenAI** and **Anthropic**, the client estimates USD from API **`usage`** token counts × approximate **standard-tier** per-million rates for known models (for example **`gpt-4o-mini`**, **`gpt-4o`**, **`claude-sonnet-4-20250514`**, **`claude-haiku-4-5-…`**). Dated snapshots match; a newer version id that only shares a shorter prefix stays at **0** until added in **`internal/models/cost.go`**. Verify against [OpenAI pricing](https://openai.com/api/pricing/) and [Anthropic pricing](https://platform.claude.com/docs/en/about-claude/pricing). Cache-read and batch rates are not applied.

### Layout

The whole project is one `.agent` file — [**`examples/example1/main.agent`**](../examples/example1/main.agent):

```text
example1/
  main.agent            # policy, native tool, support_writer agent, and support_snippet workflow
```

```
policy default {
    execution {
        maxTotalCostUsd 5
        maxWallClockSeconds 300
    }
}

tool helper {
    type native
    safety {
        sideEffects false
    }
}

agent support_writer {
    model openai/gpt-4o-mini
    policy default
    constraints {
        timeoutSeconds 60
    }
    instructions """
    You draft short customer-facing email lines for a storefront.
    You receive JSON in the user message: product name and a return-policy line from internal systems.
    Respond with one JSON object only (no markdown, no code fences).
    Use exactly this shape: {"product": "<the product name, echoed back>", "subject": "<=8 words>", "line": "<=25 words, friendly>"}
    """
}

workflow support_snippet(input: any) policy default {
    context = helper.echo(product: input.product, policy_line: "30-day returns on all SKUs; free outbound shipping on defects.")
    snippet = support_writer(product: context.echo.product, return_policy: context.echo.policy_line)
    return snippet
}
```

**Switching to Anthropic (Claude)** — point the agent at **`anthropic/<model id>`**; `anthropic` is a built-in namespace whose key comes from **`env:ANTHROPIC_API_KEY`**, and the runtime calls Anthropic’s [**Messages API**](https://docs.anthropic.com/en/api/messages) (`POST /v1/messages`). No declaration is needed for the built-in — just `model anthropic/claude-sonnet-4-20250514`.

**Identity-linked keys:** if your `ANTHROPIC_API_KEY` is identity-linked (not scoped to one workspace), Anthropic returns `HTTP 400: anthropic-workspace-id is required …`. Declare a `provider` alias to add **`workspaceIdFrom`** (same `env:VAR` form as `apiKeyFrom`); the adapter sends it as the `anthropic-workspace-id` header:

```
provider anthropic {
    type anthropic
    apiKeyFrom "env:ANTHROPIC_API_KEY"
    workspaceIdFrom "env:ANTHROPIC_WORKSPACE_ID"
}
```

**Structured JSON output:** there is no `response_format: json_object` equivalent in this adapter — agents rely on **instructions** (one JSON object, no markdown fences, as above). If you give the agent an `output <Type>` with a `schemas/<Type>.json`, the engine still validates the assistant text as JSON after generation.

The workflow composes the echo step’s payload into the model via `context.echo.…` (which lowers to `${steps.context.output.echo.…}`; see §13.1 in **`DESIGN_DOC.md`**). Because it references **`input.product`**, `terfyn run workflow/support_snippet` **requires `--input product=...`**; otherwise interpolation fails with `undefined path "input.product"`. For a zero-argument demo, replace `input.product` on the `context` step with a literal (e.g. `product: "ACME USB-C hub"`) and thread `context.echo.product` onward.

### Commands

If you copied the files to another folder, point **`--project`** at that path instead. For the [**in-repo example**](../examples/example1/), from the **repository root** use **`examples/example1`** (the directory path), not only the project name **`example1`**.

```bash
export OPENAI_API_KEY="sk-..."   # required for any step that calls the model

terfyn validate --project examples/example1
terfyn plan   --project examples/example1
terfyn apply  --project examples/example1 --auto-approve

# Checked-in example1 workflow uses ${input.product} on the context step:
terfyn run workflow/support_snippet --project examples/example1 --input product="ACME USB-C hub"

# After switching the workflow to a literal product + steps.context... (see above), you can omit --input.
```

Default **`run`** output is still **Run ID + status**. To see the workflow **`spec.output`** object ( **`product`**, **`subject`**, **`line`**, etc.):

```bash
terfyn logs --run <run-id> --project examples/example1
```

After the trace table, the CLI prints **Workflow output (from spec.output)** as indented JSON when the run succeeded and **`output_json`** is non-empty.

Or list recent runs as JSON (includes **`output`** on each run):

```bash
terfyn logs -o json --project examples/example1
```

**`terfyn logs --run <id> -o json`** also includes top-level **`input`**, **`output`**, and **`workflowName`** alongside **`events`**. Chained events include **`prevHash`** and **`hash`** when present (issue #116).

To verify trace integrity after a run:

```bash
terfyn audit verify --project examples/example1 --run <run-id>
```

See [`docs/AUDIT_CHAIN.md`](AUDIT_CHAIN.md).

Optional: give the agent an **`output <Type>`** with a matching `schemas/<Type>.json` so replies are validated with JSON Schema; see `internal/engine/testdata/wfproj/schemas/` and **`DESIGN_DOC.md`**.

---

## 5. Environment overlay

Declare an **`Environment`** resource and pass **`-e` / `--env`** to `validate`, `plan`, or `apply` when you want overrides (for example stricter or looser policy limits).

A checked-in **dev / staging / prod** overlay project lives under [**`examples/env-overlays/`**](../examples/env-overlays/README.md): `validate -e <env>`, `apply -e dev`, then `plan -e prod --from-env dev` for the promotion **risk delta**.

Declare the overlay in `.agent` (in any `.agent` file under the project root):

```
environment staging {
    overrides {
        policies {
            default {
                execution {
                    maxWallClockSeconds 600
                }
            }
        }
    }
}
```

Then select it with `-e`:

```bash
terfyn validate --project my-agent-system -e staging
```

---

## 6. Model reference cheat sheet

An agent selects its model with `model <provider>/<name>`:

| `model` (agent) | Meaning |
|-----------------|---------|
| `mock/gpt-4` | Deterministic mock string (no network) |
| `openai/gpt-4o-mini` | OpenAI API model id `gpt-4o-mini` via the built-in `openai` namespace |

The segment before **`/`** is a provider namespace. Built-in namespaces (`anthropic`, `openai`, `gemini`, `grok`, `kimi`, `mock`) need no declaration and read their credential from the conventional environment variable; declare a top-level `provider <alias> { … }` only for a custom endpoint or credential. An unknown provider fails at runtime with an error from the model registry.

---

## 7. GitHub Actions

To run **`terfyn`** on **`pull_request`** (install binary, **`validate` / `plan` / `apply` / `run`**,
with **`--approve tool.github.pull_request.post_comment`** so a real review comment is posted), see
[**`GITHUB_ACTIONS.md`**](GITHUB_ACTIONS.md) and the template under
[**`examples/pr-review-github-actions/`**](../examples/pr-review-github-actions/README.md) (a `.agent`
project — **`main.agent`** — with **OpenAI `gpt-4o-mini`** and the Actions template). The
template also appends a **job summary** (`GITHUB_STEP_SUMMARY`), optional **Actions cache** for the
SQLite file, and an optional **`gh pr comment`** pointer job (skipped by default when disabled). In
this repo the PR workflow is **`.github/workflows/terfyn-pr-review.yml`**; manual publish for an
arbitrary **`owner` / `repo` / `number`** is **`.github/workflows/terfyn-pr-review-publish.yml`**.

Fixture-style **`terfyn test`** (no API keys) is **[`examples/regression-test/`](../examples/regression-test/README.md)** with sample job **`.github/workflows/terfyn-test.yml`**. See **[`TESTING.md`](TESTING.md)**.

---

## 8. Bounded multi-agent control (`.agent`)

[**`examples/implement-review-loop/`**](../examples/implement-review-loop/README.md) is the flagship
`.agent` program: an **Implementer** and an independent **Reviewer** pass a structured `CodingState`
through a bounded `while … limit 3`, authored entirely in **`.agent`** (agent prompts in
`instructions`, typed `CodingState` input/output, and per-agent capability grants) with only tool /
policy / project **configuration** left in YAML.

It demonstrates *deterministic bounded control around nondeterministic agents*:

- the loop runs **at most three** implement/review rounds (the bound is explicit in source and
  enforced by the runtime);
- the Reviewer is granted `read_file` + `run_tests` but **not** `write_file`, so a Reviewer that
  attempts to write is **denied by capability, not by its prompt**;
- `terfyn plan` surfaces granting the Reviewer `write_file` as an **`AUTONOMOUS authority WIDENED`**
  risk — reviewable before `apply`.

```bash
terfyn validate --project examples/implement-review-loop
terfyn plan     --project examples/implement-review-loop
```

The example's README walks through the capability boundary and the plan authority-widening output;
`mock/gpt-4` keeps it reproducible with no API keys.

---

## 9. Same `.agent` under an external runtime (`--runtime claude-code`)

[**`examples/external-runtime-reviewer/`**](../examples/external-runtime-reviewer/README.md) is the
flagship for the external agent-runtime epic ([#335](https://github.com/Terfyn/terfyn/issues/335)):
the **same** reviewed `.agent` runs on Terfyn's own engine **or** an external CLI agent
(`--runtime claude-code`), and the authority is identical either way — *the runtime is replaceable;
the authority is not.*

A read-only **Reviewer** is granted `read_file` + `run_tests`. The `workspace` tool also declares
`write_file`, but the Reviewer is not granted it, so under the external runtime the per-run Terfyn
MCP server's `tools/list` is exactly `{ read_file, run_tests }` — `write_file` is never advertised
and the external model **cannot select it**.

```bash
terfyn plan --project examples/external-runtime-reviewer   # effect bound: workspace.write "unreachable"
terfyn test --project examples/external-runtime-reviewer   # forbidEffect Reviewer → workspace.write: pass
```

The README reproduces "the model literally cannot select the operation" offline via `plan` + `test`,
and shows the `--runtime claude-code` run. See [`EXTERNAL_RUNTIME.md`](EXTERNAL_RUNTIME.md) for the
AgentRuntime boundary and the grant-is-not-a-builtin rule, and
[ADR 006](adr/006-external-agent-runtimes.md) for the `RuntimeTarget` decision.
