# Examples

Short, runnable patterns for **`apiVersion: agentic.dev/v0`**. For the full YAML spec, CLI behaviour, and field semantics, see [**`DESIGN_DOC.md`**](DESIGN_DOC.md). For **`agentctl test`** fixture format, see [**`TESTING.md`**](TESTING.md).

A checked-in copy of the **OpenAI `support_snippet`** project from **section 4** lives under [**`examples/example1/`**](../examples/example1/). Its **`metadata.name`** is **`example1`**, matching that folder. From the repository root, pass **`--project examples/example1`** to **`agentctl`** (or **`cd` there** and use **`--project .`**).

### Formatting YAML (`agentctl fmt`)

Normalize indentation (2 spaces) for **`project.yaml`** / **`project.yml`** and every file in **`spec.imports`** (same closure as validate/load). **`--check`** exits **1** if any file would change (CI). **YAML comments may be lost** on rewrite—commit or branch before running.

```bash
agentctl fmt --project my-agent-system
agentctl fmt --check --project .
```

---

## 1. Scaffold with `agentctl init`

```bash
agentctl init my-agent-system
```

Creates a directory layout like:

```text
my-agent-system/
  project.yaml
  policies/default.yaml
  tools/helper.yaml
  workflows/hello.yaml
```

Sections **2–3** mirror what `init` creates. **Section 4** is a separate **`gpt-4o-mini`** project layout you can copy beside or instead of the scaffold.

---

## 2. Root `project.yaml` (mock model, local-only)

`spec.imports` lists YAML files relative to the project root. `defaults.model` uses the form **`namespace/model_id`**, where **`namespace`** matches a key under `spec.providers.models`.

Optional **`defaults.runtime`** sets where agents and workflows run in MVP: only **`local`** is valid (or omit for implicit local). Resources that omit **`spec.runtime`** inherit this value when the merged project graph is normalized.

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
```

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
agentctl validate --project my-agent-system
agentctl plan   --project my-agent-system
agentctl apply  --project my-agent-system --auto-approve
agentctl run    workflow/hello --project my-agent-system
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
- **`stdio`** and **`http`** are mutually exclusive in **`spec.mcp`**: set **`command`** only for stdio, **`url`** only for HTTP (validated at `agentctl validate`).
- Workflow trace events for tool steps record **`uses`** and cost, not HTTP headers or resolved env values; keep custom logging of MCP traffic free of secrets.

---

## 4. Real OpenAI example (`gpt-4o-mini`)

This is a small but **end-to-end** project: a **native echo** step supplies fixed “policy” text, then **`gpt-4o-mini`** drafts a one-line customer reply. You need a valid **[OpenAI API key](https://platform.openai.com/api-keys)** and outbound **HTTPS** to `api.openai.com`.

**Repo copy:** [**`examples/example1/`**](../examples/example1/) — **`agentctl validate --project examples/example1`** from the repo root, or **`agentctl validate --project .`** after **`cd examples/example1`**.

The runtime calls OpenAI’s **`/v1/chat/completions`** endpoint. The agent **must** answer with a **single JSON object** (no markdown fences); the engine parses that object and exposes its fields to **`spec.output`**.

**`totalCostUsd` on runs** is accumulated from each step’s reported cost. Native tools report **0**. For **OpenAI**, the client estimates USD from the API **`usage`** token counts × approximate per-million rates for known models (**`gpt-4o-mini`**, **`gpt-4o`**, and dated variants such as **`gpt-4o-mini-…`**). Other model ids stay at **0** until their rates are added in code; see **`internal/models/openai_cost.go`** and verify against [OpenAI pricing](https://openai.com/api/pricing/).

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

**Zero-argument demo.** To run **`agentctl run workflow/support_snippet`** with no **`--input`**, put a literal product on the first step and thread it through **`steps.context.output.echo`** (the checked-in [**`examples/example1/`**](../examples/example1/) tree uses **`${input.product}`** instead, so it **requires** **`--input product=...`** unless you edit the YAML):

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

agentctl validate --project examples/example1
agentctl plan   --project examples/example1
agentctl apply  --project examples/example1 --auto-approve

# Checked-in example1 workflow uses ${input.product} on the context step:
agentctl run workflow/support_snippet --project examples/example1 --input product="ACME USB-C hub"

# After switching the workflow to a literal product + steps.context... (see above), you can omit --input.
```

Default **`run`** output is still **Run ID + status**. To see the workflow **`spec.output`** object ( **`product`**, **`subject`**, **`line`**, etc.):

```bash
agentctl logs --run <run-id> --project examples/example1
```

After the trace table, the CLI prints **Workflow output (from spec.output)** as indented JSON when the run succeeded and **`output_json`** is non-empty.

Or list recent runs as JSON (includes **`output`** on each run):

```bash
agentctl logs -o json --project examples/example1
```

**`agentctl logs --run <id> -o json`** also includes top-level **`input`**, **`output`**, and **`workflowName`** alongside **`events`**.

Optional: add **`spec.output.schema`** on the agent (path relative to the project root) so replies are validated with JSON Schema; see `internal/engine/testdata/wfproj/schemas/` and **`DESIGN_DOC.md`**.

---

## 5. Environment overlay

Declare an **`Environment`** resource and pass **`-e` / `--env`** to `validate`, `plan`, or `apply` when you want overrides (for example stricter or looser policy limits).

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
agentctl validate --project my-agent-system -e staging
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

To run **`agentctl`** on **`pull_request`** (install binary, **`validate` / `plan` / `apply` / `run`**,
and treating policy denial exit **5** as success when comments are intentionally gated), see
[**`GITHUB_ACTIONS.md`**](GITHUB_ACTIONS.md) and the template under
[**`examples/pr-review-github-actions/`**](../examples/pr-review-github-actions/README.md) (includes
**`project.yaml`** with **OpenAI `gpt-4o-mini`** and the Actions template). The
template also appends a **job summary** (`GITHUB_STEP_SUMMARY`), optional **Actions cache** for the
SQLite file, and an optional **`gh pr comment`** pointer job (skipped by default when disabled). In
this repo the PR workflow is **`.github/workflows/agentctl-pr-review.yml`**; manual publish with
**`--approve`** is **`.github/workflows/agentctl-pr-review-publish.yml`** so PR runs do not list a
skipped publish job.
