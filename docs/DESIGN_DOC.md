# Agentic Control Plane

## Design Document v0

## 0. Summary

This project is a **declarative control plane for agent systems**.

The goal is not to be another Python agent framework.

The goal is to let teams define agents, tools, workflows, policies, and environments as **versioned YAML**, then:

* validate
* diff
* plan
* apply
* run
* trace
* govern

them like real systems.

The closest mental model is:

* **Terraform** for desired state and plan/apply
* **Kubernetes** for declarative resources and reconciliation
* **GitOps** for versioned, reviewable changes
* **OpenAPI** for explicit contracts

This document defines:

1. Go project structure
2. YAML spec v0
3. CLI UX and commands
4. internal engine architecture
5. MVP vs end goal

---

# 1. Problem Statement

Today, most agent systems are built as:

* Python/JS code
* prompts embedded in source
* tool bindings hidden in runtime code
* weak contracts
* unclear permissions
* poor change review
* little or no drift detection
* little or no governance

This creates:

* prompt spaghetti
* hidden behavior changes
* weak reproducibility
* hard reviews
* weak deployment discipline
* poor portability across runtimes/providers

We want a system where teams can say:

> “This is the desired shape of my agent system.”

And the platform can answer:

* Is it valid?
* What changed?
* What risk changed?
* What will be applied?
* What is deployed?
* What drifted?
* What happened during execution?

---

# 2. Non-Goals

This project is **not**:

* a foundation model runtime
* a model serving system
* a training platform
* an attempt to standardize chain-of-thought
* a replacement for every orchestration framework
* a magic auto-agent builder

This project does **not** try to define:

* exact internal reasoning behavior
* latent planner internals
* hidden model state
* model training/inference kernels

It defines the **control plane** around agent systems.

---

# 3. Core Principles

## 3.1 Declarative first

Users define desired state in YAML.

## 3.2 Contracts over vibes

Inputs, outputs, tools, permissions, and policies should be explicit.

## 3.3 Separate control-plane state from runtime state

Deployment state and execution traces are different things.

## 3.4 Portable by default

Specs should not be hard-coupled to one runtime.

## 3.5 Reviewable changes

Behavior, cost, permissions, and policy changes should be diffable.

## 3.6 Safe by default

Tool permissions, approvals, budgets, and policy limits must be first-class.

## 3.7 Start small, grow cleanly

MVP should be local and simple. End state can add remote runtimes and reconciliation.

---

# 4. Conceptual Model

The system manages these resource types:

* **Project**
* **Agent**
* **Tool**
* **Workflow**
* **Policy**
* **Environment**
* **ModelProvider**
* **MemoryStore** later
* **RuntimeTarget** later
* **Module** later

Each resource has:

* `apiVersion`
* `kind`
* `metadata`
* `spec`

This is intentionally Kubernetes-like because agent systems are graph-shaped and nested.

---

# 5. Go Project Structure

## 5.1 High-level layout

```text
agentctl/
  cmd/
    agentctl/
      main.go

  internal/
    app/
      app.go
      wiring.go

    cli/
      root.go
      validate.go
      plan.go
      apply.go
      diff.go
      run.go
      logs.go
      inspect.go
      test.go

    spec/
      types.go
      kinds.go
      loader.go
      parser.go
      normalize.go
      defaults.go
      validator.go
      refs.go
      errors.go

    schema/
      jsonschema.go
      registry.go
      validate.go

    project/
      loader.go
      resolver.go
      graph.go

    plan/
      planner.go
      diff.go
      risk.go
      cost.go
      output.go

    state/
      store.go
      models.go
      sqlite/
        store.go
        migrations.go
      memory/
        store.go

    apply/
      applier.go
      executor.go
      checkpoint.go

    runtime/
      runtime.go
      local/
        runtime.go
        runner.go
      interfaces/
        tool_runtime.go
        agent_runtime.go
        workflow_runtime.go

    engine/
      workflow.go
      steps.go
      interpolation.go
      execution.go
      approvals.go
      retries.go
      timeout.go

    tools/
      registry.go
      mcp/
        client.go
        transport_stdio.go
        transport_http.go
      http/
        client.go
      native/
        registry.go

    models/
      registry.go
      openai/
        client.go
      anthropic/
        client.go
      local/
        client.go

    policy/
      engine.go
      evaluator.go
      approvals.go
      budget.go
      permissions.go

    trace/
      recorder.go
      events.go
      reader.go

    logs/
      printer.go
      formatter.go

    testkit/
      runner.go
      fixtures.go
      assertions.go

    module/
      resolver.go
      lockfile.go

    render/
      yaml.go
      json.go
      table.go

    util/
      fs.go
      ids.go
      clock.go
      errors.go
      slices.go

  api/
    proto/
      controlplane.proto
      execution.proto

  pkg/
    sdk/
      types.go

  examples/
    minimal/
    pr-review/
    incident-triage/

  docs/
    spec-v0.md
    architecture.md

  scripts/
    generate.sh
    lint.sh
    test.sh

  migrations/
    sqlite/
    postgres/

  go.mod
  go.sum
  Makefile
```

---

## 5.2 Package responsibilities

### `cmd/agentctl`

Binary entrypoint.

### `internal/cli`

Command definitions, flag parsing, output formatting.

### `internal/spec`

Parsing YAML resources, type definitions, defaults, normalization, reference resolution.

### `internal/schema`

JSON Schema loading and validation for structured inputs/outputs.

### `internal/project`

Loads a project directory, merges resources, resolves imports.

### `internal/plan`

Computes desired vs current state diff, plus risk/cost delta.

### `internal/state`

Stores deployment state and runtime metadata.
MVP: SQLite.
Later: Postgres backend.

### `internal/apply`

Takes a plan and mutates runtime/control-plane state.

### `internal/runtime`

Runtime abstraction.
MVP: local runtime only.
Later: remote runtimes.

### `internal/engine`

Workflow execution engine, step orchestration, retries, interpolation.

### `internal/tools`

Tool abstraction and integrations.
MVP: native mock tools + MCP stdio.
Later: HTTP, gRPC, plugins.

### `internal/models`

Model abstraction and providers.

### `internal/policy`

Permission checks, budget checks, approval rules, safety gates.

### `internal/trace`

Structured execution events and trace persistence.

### `internal/audit`

Tamper-evident hash chain for `trace_events` (issue #116): canonical serialization, append-time hashing, and run-level verification.

### `internal/testkit`

Fixture-driven workflow tests.

### `internal/module`

Later feature for reusable modules and lockfiles.

### `api/proto`

Optional internal control-plane API for remote mode later.

---

# 6. Domain Model

---

## 6.1 Resource envelope

Every YAML file uses:

```yaml
apiVersion: agentic.dev/v0
kind: <Kind>

metadata:
  name: <resource-name>
  labels: {}
  annotations: {}

spec: {}
```

Rules:

* `apiVersion` required
* `kind` required
* `metadata.name` required, DNS-like identifier
* `labels` optional
* `annotations` optional
* `spec` required

---

## 6.2 Supported kinds in v0

MVP:

* `Project`
* `Agent`
* `Tool`
* `Workflow`
* `Policy`
* `Environment`

End goal later:

* `Module`
* `MemoryStore`
* `RuntimeTarget`
* `ApprovalPolicy` as separate kind
* `SecretRef`
* `Schedule`

---

# 7. YAML Spec v0

## 7.1 Project

Defines root project settings and imports.

```yaml
apiVersion: agentic.dev/v0
kind: Project

metadata:
  name: platform-assistant

spec:
  imports:
    - ./agents
    - ./tools
    - ./workflows
    - ./policies
    - ./env

  defaults:
    runtime: local
    model: openai/gpt-4.1
    policy: default

  providers:
    models:
      openai:
        type: openai
        apiKeyFrom: env:OPENAI_API_KEY
      anthropic:
        type: anthropic
        apiKeyFrom: env:ANTHROPIC_API_KEY

    tools:
      mcp:
        enabled: true

  state:
    backend: sqlite
    dsn: .agentic/state.db

  traces:
    backend: sqlite
    retentionDays: 14
```

### Notes

* `imports` are relative file or directory paths
* `defaults` apply when resources omit explicit values
* `providers` configures integrations
* `state` and `traces` are local in MVP

---

## 7.2 Agent

Defines an agent contract and runtime binding.

```yaml
apiVersion: agentic.dev/v0
kind: Agent

metadata:
  name: reviewer

spec:
  description: Reviews pull requests for correctness, security, and maintainability.

  model: openai/gpt-4.1

  instructions: |
    You are a senior code reviewer.
    Prioritize correctness, security, and maintainability.
    Cite concrete evidence from tool outputs when possible.

  tools:
    - github
    - docs

  policy: default

  memory:
    type: session
    maxMessages: 20

  constraints:
    maxIterations: 8
    timeoutSeconds: 90
    temperature: 0.2
    requireStructuredOutput: true

  input:
    schema: ./schemas/review-input.json

  output:
    schema: ./schemas/review-output.json
```

### MVP fields

* `description`
* `model`
* `instructions`
* `tools`
* `policy`
* `memory.type`
* `constraints`
* `input.schema`
* `output.schema`

### End goal additions

* few-shot examples
* tool-choice policy
* retrieval sources
* external memory refs
* model fallback chains
* cost ceilings per agent
* redaction rules
* audit annotations

---

## 7.3 Tool

Defines an external capability.

### MCP stdio tool

```yaml
apiVersion: agentic.dev/v0
kind: Tool

metadata:
  name: github

spec:
  type: mcp

  mcp:
    transport: stdio
    command: npx
    args:
      - -y
      - "@modelcontextprotocol/server-github"

  permissions:
    allow:
      - pull_requests.read
      - issues.read
      - contents.read
    deny: []

  retry:
    maxAttempts: 3
    backoff: exponential
```

### MCP HTTP tool (streamable HTTP)

Remote MCP servers may expose a single JSON-RPC endpoint over HTTP(S). Set **`transport: http`**, **`url`** to that endpoint, and optional **`headers`** (including **`env:`** tokens). **`command`** / **`args`** must not be set together with **`url`** (see validator).

```yaml
spec:
  type: mcp
  mcp:
    transport: http
    url: https://mcp.example.com/v1/mcp
    headers:
      Authorization: env:MCP_TOKEN
```

### Native HTTP tool

```yaml
apiVersion: agentic.dev/v0
kind: Tool

metadata:
  name: webhook

spec:
  type: http

  http:
    baseUrl: https://api.example.com
    headers:
      Authorization: env:API_TOKEN

  permissions:
    allow:
      - request.send

  retry:
    maxAttempts: 2
    backoff: fixed
```

### MVP tool types

* `mcp`
* `http`
* `native` mock/local

### End goal tool types

* gRPC
* queue
* SQL
* filesystem
* plugin SDK

---

## 7.4 Workflow

Defines graph execution.

```yaml
apiVersion: agentic.dev/v0
kind: Workflow

metadata:
  name: pr-review

spec:
  description: Review a pull request and post a summary.

  trigger:
    type: manual

  input:
    schema: ./schemas/pr-review-input.json

  policy: default

  steps:
    - id: fetch_pr
      uses: tool.github.pull_request.get
      with:
        repo: ${input.repo}
        number: ${input.number}

    - id: review
      agent: reviewer
      with:
        pr: ${steps.fetch_pr.output}

    - id: post_comment
      uses: tool.github.pull_request.comment
      with:
        repo: ${input.repo}
        number: ${input.number}
        body: ${steps.review.output.summary}

  output:
    value:
      summary: ${steps.review.output.summary}
      findings: ${steps.review.output.findings}
```

### MVP workflow rules

* steps execute sequentially
* each step has either `agent` or `uses`
* `with` maps inputs
* `${...}` interpolation supported
* output can map from prior step outputs
* only manual trigger in MVP

### End goal additions

* conditional steps
* loops
* fan-out/fan-in
* scheduled triggers
* event triggers
* human approval steps
* parallel branches
* subworkflows

---

## 7.5 Policy

Defines execution and governance limits.

```yaml
apiVersion: agentic.dev/v0
kind: Policy

metadata:
  name: default

spec:
  execution:
    maxWallClockSeconds: 180
    maxTotalCostUsd: 3.00
    requireStructuredOutput: true

  tools:
    forbidUnknownTools: true

  approvals:
    requiredFor:
      - tool.github.pull_request.merge
      - tool.slack.message.send

  security:
    networkAccess: restricted
    secretAccess: deny-by-default
```

### MVP

* cost ceiling
* wall clock limit
* require structured output
* forbid unknown tools
* approval-required actions

### End goal

* per-step policy overrides
* redaction policy
* PII handling rules
* tenant isolation
* prompt injection controls
* environment-specific policy inheritance

---

## 7.6 Environment

Overrides resources for a target environment.

```yaml
apiVersion: agentic.dev/v0
kind: Environment

metadata:
  name: prod

spec:
  overrides:
    agents:
      reviewer:
        model: anthropic/claude-sonnet-4
        constraints:
          timeoutSeconds: 60

    policies:
      default:
        execution:
          maxTotalCostUsd: 10.00
```

### MVP

* agent and policy overrides only

### End goal

* tool endpoint overrides
* secret binding overrides
* runtime target selection
* scheduling overrides
* provider selection overrides

---

# 8. File Layout for a User Project

```text
my-agent-system/
  project.yaml

  agents/
    reviewer.yaml
    incident.yaml

  tools/
    github.yaml
    slack.yaml

  workflows/
    pr-review.yaml
    incident-triage.yaml

  policies/
    default.yaml
    strict.yaml

  env/
    dev.yaml
    prod.yaml

  schemas/
    pr-review-input.json
    review-output.json
```

---

# 9. Validation Rules

## 9.1 Global validation

* all resources must have unique `kind/name`
* all references must resolve
* all imported paths must exist
* all schemas must be readable
* environment overrides must target existing resources

## 9.2 Agent validation

* referenced tools must exist
* referenced policy must exist
* input/output schema files must exist
* constraints must be sane
* model string must match configured provider namespace or allowed local alias

## 9.3 Tool validation

* exactly one transport block for selected `type`
* permission actions must be valid strings
* retry values must be non-negative

## 9.4 Workflow validation

* step ids must be unique
* each step must specify exactly one of `agent` or `uses`
* interpolation refs must resolve
* forward refs allowed only where dependency order is valid
* no cycles in MVP since sequential only

## 9.5 Policy validation

* budgets non-negative
* action identifiers syntactically valid
* approval actions unique

---

# 10. CLI UX and Commands

## 10.1 Philosophy

The CLI should feel like:

* Terraform in clarity
* kubectl in resource mental model
* git in inspectability

Commands should be boring, stable, and scriptable.

---

## 10.2 Core commands

## `agentctl init`

Create starter project.

```bash
agentctl init my-agent-system
```

Creates:

* project.yaml
* sample dirs
* example workflow

### MVP

yes

---

## `agentctl validate`

Validate project.

```bash
agentctl validate
agentctl validate -e prod
```

Checks:

* YAML syntax
* schema correctness
* references
* imports
* interpolation refs
* policy and permission issues

### Output example

```text
Project: platform-assistant
Environment: prod

✓ Loaded 7 resources
✓ References resolved
✓ Schemas valid
✓ Workflow pr-review valid

Validation successful
```

### MVP

yes

---

## `agentctl plan`

Show desired vs current diff.

```bash
agentctl plan
agentctl plan -e prod
```

### Output example

```text
Plan: 2 to add, 1 to change, 0 to delete

+ create Agent/reviewer
+ create Workflow/pr-review
~ update Policy/default
    maxTotalCostUsd: 3.00 -> 10.00

Risk delta:
- cost ceiling increased
- approval scope unchanged
- no new write permissions
```

### MVP

partial

MVP plan supports:

* create/update detection
* field diff
* basic risk summary

MVP does not support:

* remote drift detection
* advanced behavioral estimates

---

## `agentctl apply`

Apply desired state.

```bash
agentctl apply
agentctl apply -e prod
agentctl apply --auto-approve
```

Behavior:

* runs validate
* computes plan
* prompts for approval unless `--auto-approve`
* writes deployment state

### MVP

yes, but local only

---

## `agentctl diff`

Show detailed resource diff.

```bash
agentctl diff
agentctl diff Agent/reviewer
```

### MVP

optional but strongly recommended

---

## `agentctl run`

Execute workflow ad hoc.

```bash
agentctl run workflow/pr-review --input repo=acme/api --input number=42
agentctl run workflow/pr-review --input-file input.json
```

Behavior:

* loads deployed or local desired config depending on mode
* validates input against workflow schema
* executes steps
* stores trace

### MVP

yes

---

## `agentctl logs`

Show execution traces.

```bash
agentctl logs
agentctl logs --run <run-id>
agentctl logs --workflow pr-review
```

### MVP

yes, basic trace/event view

---

## `agentctl audit`

Verify tamper-evident hash chains over `trace_events`.

```bash
agentctl audit verify
agentctl audit verify --run <run-id>
agentctl audit verify --limit 200
```

Re-derives each stored `hash` and checks `prev_hash` linkage. Without `--run`, scans recent runs only (`--limit`, default 50, max 500). Pre-migration rows without hashes are reported as **unchained** and do not fail verification. Exit **1** on chain break. See [`docs/AUDIT_CHAIN.md`](AUDIT_CHAIN.md).

### MVP

yes (issue #116)

---

## `agentctl inspect`

Print normalized resource.

```bash
agentctl inspect Workflow/pr-review
agentctl inspect Agent/reviewer -o yaml
```

Useful for debugging defaults and env overrides.

### MVP

optional but useful

---

## `agentctl test`

Run fixture-based tests.

```bash
agentctl test
agentctl test workflow/pr-review
```

### Test file example

```yaml
workflow: pr-review

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

### MVP

stretch MVP or early post-MVP

---

## `agentctl fmt`

Normalize YAML formatting.

```bash
agentctl fmt
```

### MVP

nice-to-have

---

## `agentctl state`

Inspect stored state.

```bash
agentctl state list
agentctl state show Agent/reviewer
```

### MVP

optional

---

# 11. CLI UX Details

## 11.1 Common flags

```text
-e, --env <name>         environment override
-o, --output <fmt>       table|json|yaml
--project <path>         project root
--state <path>           explicit state DB path
--no-color               disable color output
```

## 11.2 Exit codes

* `0` success
* `1` generic failure
* `2` validation error
* `3` plan/apply conflict
* `4` execution error
* `5` policy denial

---

# 12. Internal Architecture of the Engine

## 12.1 Top-level architecture

```text
YAML Project
   ↓
Loader / Parser
   ↓
Normalization / Defaults
   ↓
Reference Resolution
   ↓
Validation
   ↓
Desired State Graph
   ↓
Planner
   ↓
Apply
   ↓
Stored Deployment State

Run Workflow
   ↓
Execution Engine
   ↓
Policy Engine
   ↓
Model + Tool Adapters
   ↓
Trace Recorder
   ↓
Runtime State
```

---

## 12.2 Core subsystems

## A. Spec subsystem

Responsibilities:

* load YAML files
* decode into typed structs
* normalize defaults
* resolve imports
* resolve references
* return canonical in-memory project graph

Key types:

```go
type ResourceID struct {
    Kind string
    Name string
}

type Project struct {
    Meta        Metadata
    Spec        ProjectSpec
    Agents      map[string]*Agent
    Tools       map[string]*Tool
    Workflows   map[string]*Workflow
    Policies    map[string]*Policy
    Environments map[string]*Environment
}
```

---

## B. Planner subsystem

Responsibilities:

* compare desired project state against stored deployment state
* compute create/update/delete operations
* compute human-readable diffs
* compute risk summary

Key output:

```go
type Plan struct {
    Operations []Operation
    Risk       RiskSummary
}

type Operation struct {
    Action   string // create, update, delete
    Target   ResourceID
    Diff     []FieldChange
}
```

### MVP risk summary

* new write permissions
* removed approvals
* model changes
* cost cap changes

### End goal risk summary

* behavioral contract widening
* policy relaxations
* prompt changes with semantic classification
* runtime target change impact

---

## C. State subsystem

Two different state domains:

### 1. Deployment state

Tracks what has been applied.

Example records:

* resource kind/name
* normalized spec hash
* applied timestamp
* env target
* version

### 2. Runtime state

Tracks workflow runs.

Example records:

* run id
* workflow name
* start/end
* status
* step events
* tool calls
* token/cost summary
* errors

### MVP storage

SQLite for both.

### End goal

Postgres for team/shared mode.

---

## D. Apply subsystem

Responsibilities:

* take a plan
* confirm/persist operations
* update deployment state
* optionally prepare runtime-specific artifacts later

MVP apply does **not** deploy to an external cluster.
It records local deployed desired state.

That is enough to establish plan/apply discipline.

---

## E. Execution engine

Responsibilities:

* execute workflows
* resolve step inputs
* call tools
* invoke agents
* enforce retries/timeouts
* collect outputs
* produce final workflow output

### MVP execution model

* sequential steps only
* local execution only
* no background daemons
* no reconciliation loop

### Engine flow

1. load workflow
2. validate runtime input
3. initialize run context
4. for each step:

   * resolve interpolations
   * enforce policy
   * execute tool or agent
   * validate output if configured
   * record trace
5. compute workflow output
6. persist run result

---

## F. Agent runtime adapter

Responsibilities:

* assemble prompt payload
* attach tools
* invoke provider
* return structured output

Abstraction:

```go
type ModelClient interface {
    Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
}
```

Model contract (issue #156):

* `GenerateRequest` — `Model`, `Messages`, optional `Tools []ToolDef`, `ToolChoice` (`auto` | `none` | `required`; zero value = `auto`).
* `GenerateResponse` — `Content`, optional `ToolCalls []ToolCall`, `StopReason` (`end_turn` | `tool_use` | `max_tokens`), `Meta`.
* `ChatMessage` — `Role`, `Content`, optional `ToolResults []ToolResult` for returning tool output to the model.
* `GenerateMeta` — `DurationMs`, `PromptTokens`, `CompletionTokens`, `CostUSD`.

Provider adapters map these neutral shapes to OpenAI `tools` / `tool_calls` and Anthropic `tools` / `tool_use` / `tool_result`.

MVP:

* OpenAI-compatible
* Anthropic optional
* mock provider for tests

---

## G. Tool runtime adapter

Responsibilities:

* resolve tool name to executable transport
* enforce permissions
* execute operation
* normalize result

Abstraction:

```go
type ToolExecutor interface {
    Call(ctx context.Context, req ToolCallRequest) (ToolCallResponse, error)
}
```

MVP:

* MCP stdio
* HTTP
* mock/native

---

## H. Policy engine

Responsibilities:

* decide whether a workflow/step/tool call is allowed
* enforce budgets/timeouts
* gate approval-required actions

Abstraction:

```go
type PolicyEvaluator interface {
    CheckRun(ctx context.Context, run RunContext) error
    CheckStep(ctx context.Context, step StepContext) error
    CheckToolCall(ctx context.Context, call ToolCallContext) error
}
```

### MVP checks

* workflow wall-clock budget
* total cost ceiling
* no unknown tools
* approval-required actions denied unless explicitly approved

### End goal checks

* environment-sensitive rules
* tenant rules
* sensitive output handling
* prompt injection mitigation hooks
* egress restrictions

---

## I. Trace recorder

Responsibilities:

* append structured events
* persist for logs and debugging
* support replay-ish inspection

Event examples:

```go
type TraceEvent struct {
    RunID     string
    Timestamp time.Time
    Type      string
    StepID    string
    Message   string
    Data      map[string]any
}
```

Event types (issue #115 closed taxonomy, `TaxonomyVersion` 1):

* run_started, run_finished, run_error
* llm_completion
* tool_selection, tool_execution
* hitl_request_created, hitl_decision_submitted, hitl_resolution_applied
* memory_read, memory_write (reserved)
* system_error

Legacy dot-notation types (`run.started`, `tool.called`, …) are normalized to the above on read and by SQLite migration `006`.

Issue #116 adds a **tamper-evident hash chain** per run: each persisted event stores `prev_hash` and `hash` over canonical (redacted) fields. See [`docs/AUDIT_CHAIN.md`](AUDIT_CHAIN.md).

---

# 13. Execution Semantics

## 13.1 Interpolation

Supported syntax:

* `${input.foo}`
* `${steps.fetch_pr.output}`
* `${steps.review.output.summary}`

MVP:

* dot path lookup only
* no expressions
* no functions
* no loops in interpolation

This should stay simple.
Do not invent a scripting language.

---

## 13.2 Step types

### Agent step

```yaml
- id: review
  agent: reviewer
  with:
    pr: ${steps.fetch_pr.output}
```

### Tool step

```yaml
- id: fetch_pr
  uses: tool.github.pull_request.get
  with:
    repo: ${input.repo}
    number: ${input.number}
```

MVP step result shape:

```json
{
  "output": {},
  "meta": {
    "durationMs": 1200,
    "costUsd": 0.02
  }
}
```

---

## 13.3 Output validation

If a step or agent has output schema, validate returned output.
Failure should fail the step unless policy later allows soft-fail.

MVP:
hard fail

---

## 13.4 Retries

Tool retries in MVP:

* configured per tool
* only on retryable transport/provider errors

Agent retries in MVP:

* off by default
* optional single retry on transient provider failure

Do not retry semantic failure blindly.

---

# 14. State Model

## 14.1 Deployment state schema

Suggested tables:

### `applied_resources`

* `kind`
* `name`
* `env`
* `spec_hash`
* `normalized_spec_json`
* `applied_at`

### `applied_projects`

* `project_name`
* `env`
* `version`
* `applied_at`

---

## 14.2 Runtime state schema

### `runs`

* `run_id`
* `workflow_name`
* `env`
* `status`
* `started_at`
* `finished_at`
* `input_json`
* `output_json`
* `error_text`
* `total_cost_usd`

### `run_steps`

* `run_id`
* `step_id`
* `status`
* `started_at`
* `finished_at`
* `input_json`
* `output_json`
* `error_text`
* `cost_usd`

### `trace_events`

* `run_id`
* `seq`
* `timestamp`
* `type`
* `actor_type` (issue #115)
* `step_id`
* `data_json`
* `tenant_id`, `thread_id`, `actor_id` (issue #111; copied from parent run)
* `prev_hash`, `hash` (issue #116; nullable for pre-migration rows)

Per-run hash chain: `hash = SHA-256(canonical_event ‖ prev_hash)`. First chained event in a run links to a run-scoped genesis anchor. See [`docs/AUDIT_CHAIN.md`](AUDIT_CHAIN.md).

---

# 15. Modules and Registry

## MVP

No modules.

## End goal

Modules should allow reuse.

Example:

```yaml
module:
  source: github.com/acme/agent-modules/pr-reviewer
  version: 0.2.1

inputs:
  model: openai/gpt-4.1
  githubTool: github
```

Needs:

* lockfile
* version resolution
* input schema
* module output exposure
* integrity checks

Registry later:

* public or private modules
* reusable workflows
* policy packs
* tool packs

---

# 16. Remote Runtime and Reconciliation

## MVP

No controller. No daemon. No remote cluster.

`apply` only writes local deployed state.

This is enough to prove:

* spec
* validation
* plan
* run
* trace

## End goal

Add remote runtime support.

Possible modes:

* local
* server mode
* Kubernetes-backed
* Temporal-backed
* worker pool mode

At that stage, reconciliation becomes meaningful:

* desired resources stored centrally
* controller compares desired vs actual
* controller converges state
* drift detectable remotely

This is **post-MVP**.

---

# 17. Testing Strategy

## 17.1 Unit tests

* spec parser
* reference resolution
* interpolation
* planner diff
* policy checks
* state store

## 17.2 Integration tests

* run sample workflow locally
* mock model/tool providers
* SQLite state/traces

## 17.3 Golden tests

CLI output for:

* validate
* plan
* diff

## 17.4 Fixture tests

Workflow-level tests via YAML fixtures.

---

# 18. MVP Scope

## Included in MVP

### Spec

* Project
* Agent
* Tool
* Workflow
* Policy
* Environment

### Runtime

* local only

### Tools

* MCP stdio
* HTTP
* mock/native

### Models

* at least one provider
* mock provider for tests

### CLI

* `init`
* `validate`
* `plan`
* `apply`
* `run`
* `logs`

### Engine

* sequential workflows
* interpolation
* schema validation
* basic policy enforcement
* trace recording

### State

* SQLite
* deployment state
* runtime traces

### Output

* table + json

---

## Explicitly out of MVP

* modules
* registry
* reconciliation controller
* remote shared state
* parallel execution
* scheduled/event triggers
* subworkflows
* loops/conditionals
* rich approval workflows
* distributed execution
* plugin SDK
* advanced drift detection
* semantic change classification
* multi-tenant authn/authz

---

# 19. End Goal

The end-state system should support:

* declarative multi-agent systems
* environment-aware config
* plan/apply/diff/drift
* reusable modules
* policy packs
* remote control plane
* centralized state
* controller reconciliation
* multiple runtimes
* team collaboration
* approval workflows
* event and schedule triggers
* observability and auditability
* registry ecosystem

In other words:

> a real control plane for agent systems

not just a local runner.

---

# 20. Recommended Implementation Phases

## Phase 1

Foundations.

* resource structs
* YAML loader
* validation
* project graph
* SQLite state
* local runtime
* sequential workflow execution
* model/tool interfaces
* trace recorder
* core CLI

## Phase 2

Deployment discipline.

* better plan output
* apply confirmation
* environment overrides
* richer policy engine
* diff command
* inspect command
* test command

## Phase 3

Reuse and governance.

* modules
* lockfile
* policy packs
* richer risk summaries
* workflow approvals

## Phase 4

Control plane.

* server mode
* remote state
* controller loop
* remote runners
* drift detection
* team auth

---

# 21. Recommended Go Libraries

Practical picks:

* CLI: `cobra`
* YAML: `gopkg.in/yaml.v3`
* config/schema helpers: `invopop/jsonschema` or JSON Schema validator libs
* SQLite: `modernc.org/sqlite` or `mattn/go-sqlite3`
* table output: `charmbracelet/lipgloss` + simple table lib
* gRPC later: `google.golang.org/grpc`
* protobuf later: `google.golang.org/protobuf`

Keep dependencies conservative.

---

# 22. Example MVP Flow

## Author

User writes:

* `project.yaml`
* `agents/reviewer.yaml`
* `tools/github.yaml`
* `workflows/pr-review.yaml`
* `policies/default.yaml`

## Validate

```bash
agentctl validate
```

## Plan

```bash
agentctl plan
```

Output:

```text
Plan: 4 to add, 0 to change, 0 to delete
+ Agent/reviewer
+ Tool/github
+ Workflow/pr-review
+ Policy/default
```

## Apply

```bash
agentctl apply
```

## Run

```bash
agentctl run workflow/pr-review --input repo=acme/api --input number=42
```

## Inspect logs

```bash
agentctl logs --workflow pr-review
```

That is enough to prove the product.

---

# 23. Final Recommendation

Build this as:

* **Go CLI**
* **YAML declarative spec**
* **SQLite local state**
* **local-first engine**
* **clear separation between deployment state and execution state**

Do **not** start with:

* server mode
* Kubernetes operator
* module registry
* remote control plane

That is how the project dies early.

The correct MVP is:

> local declarative agent systems with validate, plan, apply, run, and logs.

That is small enough to build and sharp enough to matter.
