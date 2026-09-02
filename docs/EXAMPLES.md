# Examples

Short, runnable patterns for **`apiVersion: agentic.dev/v0`**. For the full YAML spec, CLI behaviour, and field semantics, see [**`DESIGN_DOC.md`**](DESIGN_DOC.md). For **`terfyn test`** fixture format, see [**`TESTING.md`**](TESTING.md).

A checked-in copy of the **OpenAI `support_snippet`** project from **section 4** lives under [**`examples/example1/`**](../examples/example1/). Its **`metadata.name`** is **`example1`**, matching that folder. From the repository root, pass **`--project examples/example1`** to **`terfyn`** (or **`cd` there** and use **`--project .`**).

### Formatting YAML (`terfyn fmt`)

Normalize indentation (2 spaces) for **`project.yaml`** / **`project.yml`** and every file in **`spec.imports`** (same closure as validate/load). **`--check`** exits **1** if any file would change (CI). **YAML comments may be lost** on rewrite—commit or branch before running.

```bash
terfyn fmt --project my-agent-system
terfyn fmt --check --project .
```

---

## 1. Scaffold with `terfyn init`

```bash
terfyn init my-agent-system
```

Creates a directory layout like:

```text
my-agent-system/
  project.yaml
  main.agent            # the hello workflow, authored in .agent (discovered, not imported)
  policies/default.yaml
  tools/helper.yaml
```

The scaffold leads on the **`.agent`** authoring surface (ADR 002 / [ADR 003](adr/003-yaml-as-compilation-output.md)): the workflow is a `.agent` source ([grammar reference](LANGUAGE.md)), while policies, tools, and project config stay YAML. `.agent` files anywhere under the project root are discovered and compiled automatically — they are **not** listed in `spec.imports`.

YAML remains valid ingress and the compilation/interchange format, so a workflow can also be authored directly in YAML — that equivalent form is shown in **section 3** below. Sections **2–3** mirror the YAML companions `init` creates (project, policy, tool); **section 4** is a separate **`gpt-4o-mini`** project layout you can copy beside or instead of the scaffold.

---

## 2. Root `project.yaml` (mock model, local-only)

`spec.imports` lists YAML files relative to the project root. `defaults.model` uses the form **`namespace/model_id`**, where **`namespace`** matches a key under `spec.providers.models`.

Optional **`defaults.runtime`** sets where agents and workflows run: the built-in **`local`** engine (or omit for implicit local), or an external agent runtime such as **`claude-code`** (see [`EXTERNAL_RUNTIME.md`](EXTERNAL_RUNTIME.md) and section 9). Resources that omit **`spec.runtime`** inherit this value when the merged project graph is normalized.

```yaml
apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: my-agent-system
spec:
  imports:
    - ./policies/default.yaml
    - ./tools/helper.yaml
    - ./workflows/hello.yaml
  defaults:
    policy: default
    model: mock/gpt-4
    runtime: local
  providers:
    models:
      mock:
        type: mock
  limits:
    maxToolInputBytes: 262144    # 256 KiB; truncate by default
    maxToolOutputBytes: 262144   # 256 KiB
    maxCheckpointBytes: 1048576  # 1 MiB; fail closed (never truncate checkpoints)
    toolInputExceedPolicy: truncate
    toolOutputExceedPolicy: truncate
    checkpointExceedPolicy: fail
```

`spec.limits` bounds tool I/O and checkpoint bytes. Workflow and Tool resources may override individual fields. `maxStateBytes` is an alias for `maxCheckpointBytes`. When `truncate` is set, long string fields are shortened in-place (top-level keys are preserved); `fail` aborts the step. Checkpoint limits always fail closed.

---

## 3. Policy, native tool, tool-only workflow

**`policies/default.yaml`**

```yaml
apiVersion: agentic.dev/v0
kind: Policy
metadata:
  name: default
spec:
  execution:
    maxWallClockSeconds: 300
    maxTotalCostUsd: 5
```

**`tools/helper.yaml`** — `type: native` uses built-in tools (see design doc for names).

```yaml
apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: helper
spec:
  type: native
```

**`workflows/hello.yaml`** — each step sets **exactly one** of `uses` (tool) or `agent` (LLM agent).

```yaml
apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: hello
spec:
  policy: default
  steps:
    - id: greet
      uses: tool.helper.echo
      with:
        message: "hello"
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

For MCP servers exposed over **HTTP** (streamable HTTP transport: one **POST** per JSON-RPC message), set **`spec.mcp.transport: http`** and **`spec.mcp.url`** to the MCP endpoint (must be **`http://`** or **`https://`**). Optional **`spec.mcp.headers`** use the same patterns as native HTTP tools (literal values or **`env:VAR_NAME`** for secrets).

```yaml
apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: remote_mcp
spec:
  type: mcp
  mcp:
    transport: http
    url: https://mcp.example.com/v1/mcp
    headers:
      Authorization: env:MCP_BEARER_TOKEN
```

**Security**

- Prefer **HTTPS** in production. The default Go client performs **normal TLS certificate verification** against the system trust store; do not disable verification for MCP calls.
- **`stdio`** and **`http`** are mutually exclusive in **`spec.mcp`**: set **`command`** only for stdio, **`url`** only for HTTP (validated at `terfyn validate`).
- Workflow trace events for tool steps record **`uses`** and cost, not HTTP headers or resolved env values; keep custom logging of MCP traffic free of secrets.

---

## 4. Real OpenAI example (`gpt-4o-mini`)

This is a small but **end-to-end** project: a **native echo** step supplies fixed “policy” text, then **`gpt-4o-mini`** drafts a one-line customer reply. You need a valid **[OpenAI API key](https://platform.openai.com/api-keys)** and outbound **HTTPS** to `api.openai.com`.

**Repo copy:** [**`examples/example1/`**](../examples/example1/) — **`terfyn validate --project examples/example1`** from the repo root, or **`terfyn validate --project .`** after **`cd examples/example1`**.

The runtime calls OpenAI’s **`/v1/chat/completions`** endpoint. The agent **must** answer with a **single JSON object** (no markdown fences); the engine parses that object and exposes its fields to **`spec.output`**.

**`totalCostUsd` on runs** is accumulated from each step’s reported cost. Native tools report **0**. For **OpenAI** and **Anthropic**, the client estimates USD from API **`usage`** token counts × approximate **standard-tier** per-million rates for known models (for example **`gpt-4o-mini`**, **`gpt-4o`**, **`claude-sonnet-4-20250514`**, **`claude-haiku-4-5-…`**). Dated snapshots match; a newer version id that only shares a shorter prefix stays at **0** until added in **`internal/models/cost.go`**. Verify against [OpenAI pricing](https://openai.com/api/pricing/) and [Anthropic pricing](https://platform.claude.com/docs/en/about-claude/pricing). Cache-read and batch rates are not applied.

### Layout

```text
example1/
  project.yaml
  policies/default.yaml
  tools/helper.yaml
  agents/support_writer.yaml
  workflows/support_snippet.yaml
```

Reuse **`policies/default.yaml`** and **`tools/helper.yaml`** from **section 3** unchanged.

### `project.yaml`

```yaml
apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: example1
spec:
  imports:
    - ./policies/default.yaml
    - ./tools/helper.yaml
    - ./agents/support_writer.yaml
    - ./workflows/support_snippet.yaml
  defaults:
    policy: default
    model: openai/gpt-4o-mini
  providers:
    models:
      mock:
        type: mock
      openai:
        type: openai
        apiKeyFrom: env:OPENAI_API_KEY
```

**Anthropic (Claude)** — register a second namespace and point agents at **`anthropic/<model id>`** (same pattern as OpenAI). Keys use **`env:ANTHROPIC_API_KEY`**; the runtime calls Anthropic’s [**Messages API**](https://docs.anthropic.com/en/api/messages) (`POST /v1/messages`).

```yaml
  providers:
    models:
      anthropic:
        type: anthropic
        apiKeyFrom: env:ANTHROPIC_API_KEY
  defaults:
    model: anthropic/claude-sonnet-4-20250514
```

**Structured JSON output:** there is no MVP `response_format: json_object` equivalent in this adapter—agents rely on **instructions** (same as in **`agents/support_writer.yaml`**: one JSON object, no markdown fences). If you set **`spec.output.schema`**, the engine still validates the assistant text as JSON after generation.

### `agents/support_writer.yaml`

`metadata.name` is the value you use in **`agent:`** on the workflow step.

```yaml
apiVersion: agentic.dev/v0
kind: Agent
metadata:
  name: support_writer
spec:
  model: openai/gpt-4o-mini
  policy: default
  constraints:
    timeoutSeconds: 60
  instructions: |
    You draft short customer-facing email lines for a storefront.
    You receive JSON in the user message: product name and a return-policy line from internal systems.
    Respond with one JSON object only (no markdown, no code fences).
    Use exactly this shape: {"subject": "<=8 words>", "line": "<=25 words, friendly>"}
```

### `workflows/support_snippet.yaml`

The compose step passes the echo step’s payload into the model via **`${steps.context.output.echo...}`** (see §13.1 in **`DESIGN_DOC.md`**).

**CLI-driven product (requires `--input`).** If you use **`${input.product}`** anywhere in the workflow, you **must** pass **`--input product=...`** on **`run`**. Otherwise interpolation fails with **`undefined path "input.product"`** because the run input object is empty.

```yaml
apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: support_snippet
spec:
  policy: default
  steps:
    - id: context
      uses: tool.helper.echo
      with:
        product: "${input.product}"
        policy_line: "30-day returns on all SKUs; free outbound shipping on defects."
    - id: compose
      agent: support_writer
      with:
        product: "${input.product}"
        return_policy: "${steps.context.output.echo.policy_line}"
  output:
    value:
      product: ${input.product}
      subject: ${steps.compose.output.subject}
      line: ${steps.compose.output.line}
```

**Zero-argument demo.** To run **`terfyn run workflow/support_snippet`** with no **`--input`**, put a literal product on the first step and thread it through **`steps.context.output.echo`** (the checked-in [**`examples/example1/`**](../examples/example1/) tree uses **`${input.product}`** instead, so it **requires** **`--input product=...`** unless you edit the YAML):

```yaml
    - id: context
      uses: tool.helper.echo
      with:
        product: "ACME USB-C hub" # literal default; or "${input.product}" + --input product=...
        policy_line: "30-day returns on all SKUs; free outbound shipping on defects."
    - id: compose
      agent: support_writer
      with:
        product: "${steps.context.output.echo.product}"
        return_policy: "${steps.context.output.echo.policy_line}"
  output:
    value:
      product: ${steps.context.output.echo.product}
      subject: ${steps.compose.output.subject}
      line: ${steps.compose.output.line}
```

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

Optional: add **`spec.output.schema`** on the agent (path relative to the project root) so replies are validated with JSON Schema; see `internal/engine/testdata/wfproj/schemas/` and **`DESIGN_DOC.md`**.

---

## 5. Environment overlay

Declare an **`Environment`** resource and pass **`-e` / `--env`** to `validate`, `plan`, or `apply` when you want overrides (for example stricter or looser policy limits).

A checked-in **dev / staging / prod** overlay project lives under [**`examples/env-overlays/`**](../examples/env-overlays/README.md): `validate -e <env>`, `apply -e dev`, then `plan -e prod --from-env dev` for the promotion **risk delta**.

**`environments/staging.yaml`**

```yaml
apiVersion: agentic.dev/v0
kind: Environment
metadata:
  name: staging
spec:
  overrides:
    policies:
      default:
        execution:
          maxWallClockSeconds: 600
```

Add **`./environments/staging.yaml`** to **`spec.imports`** in `project.yaml`, then:

```bash
terfyn validate --project my-agent-system -e staging
```

---

## 6. Model reference cheat sheet

| `defaults.model` / `spec.model` (agent) | Meaning |
|----------------------------------------|---------|
| `mock/gpt-4` | Deterministic mock string (no network) |
| `openai/gpt-4o-mini` | OpenAI API model id `gpt-4o-mini` via `providers.models.openai` |

The segment before **`/`** must match a key under **`spec.providers.models`**. Unsupported provider types fail at runtime with an error from the model registry.

---

## 7. GitHub Actions

To run **`terfyn`** on **`pull_request`** (install binary, **`validate` / `plan` / `apply` / `run`**,
with **`--approve tool.github.pull_request.post_comment`** so a real review comment is posted), see
[**`GITHUB_ACTIONS.md`**](GITHUB_ACTIONS.md) and the template under
[**`examples/pr-review-github-actions/`**](../examples/pr-review-github-actions/README.md) (includes
**`project.yaml`** with **OpenAI `gpt-4o-mini`** and the Actions template). The
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
