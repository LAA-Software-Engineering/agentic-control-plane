# Examples

Short, runnable patterns for **`apiVersion: agentic.dev/v0`**. For the full YAML spec, CLI behaviour, and field semantics, see [**`design_doc.md`**](design_doc.md).

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

The generated files match the snippets in sections 2–4 below (with `metadata.name` set from the argument you pass to `init`).

---

## 2. Root `project.yaml` (mock model, local-only)

`spec.imports` lists YAML files relative to the project root. `defaults.model` uses the form **`namespace/model_id`**, where **`namespace`** matches a key under `spec.providers.models`.

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

## 4. OpenAI chat (real model)

The control plane currently wires **`type: openai`** to the OpenAI **`/v1/chat/completions`** API. Set a key via **`apiKeyFrom`** (MVP: **`env:VAR`** only).

**`project.yaml`** (add an `openai` provider and point defaults at an OpenAI model id):

```yaml
apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: my-agent-system
spec:
  imports:
    - ./policies/default.yaml
    - ./tools/helper.yaml
    - ./agents/assistant.yaml
    - ./workflows/chat.yaml
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

```bash
export OPENAI_API_KEY="sk-..."   # required before validate/plan/apply/run
```

**`agents/assistant.yaml`** — `metadata.name` is what workflow steps reference in **`agent:`**. The executor expects the model’s reply to be a **JSON object** (plain text, not fenced code blocks).

```yaml
apiVersion: agentic.dev/v0
kind: Agent
metadata:
  name: assistant
spec:
  model: openai/gpt-4o-mini
  policy: default
  instructions: |
    You are a concise assistant. Respond with a single JSON object only, no markdown.
    Shape: {"message": "<your reply>"}
```

**`workflows/chat.yaml`**

```yaml
apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: chat
spec:
  policy: default
  steps:
    - id: reply
      agent: assistant
      with:
        topic: "Say hello in one short sentence."
```

```bash
agentctl run workflow/chat --project my-agent-system
```

Optional: add **`spec.output.schema`** on the agent (path relative to project root) to validate the JSON against JSON Schema; see test fixtures under `internal/engine/testdata/` and **`design_doc.md`**.

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
