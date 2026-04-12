# Agentic Control Plane

[![CI](https://github.com/LAA-Software-Engineering/agentic-control-plane/actions/workflows/ci.yml/badge.svg)](https://github.com/LAA-Software-Engineering/agentic-control-plane/actions/workflows/ci.yml)
[![Go 1.22+](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/LAA-Software-Engineering/agentic-control-plane.svg)](https://pkg.go.dev/github.com/LAA-Software-Engineering/agentic-control-plane)

**Declarative YAML for agents, tools, workflows, and policies — with a Terraform-style plan/apply loop, local SQLite state, and execution traces.**

This is **not** another opaque agent framework. It is a **control plane**: you describe the *desired shape* of your agent system in versioned resources, then **validate**, **plan**, **apply**, **run**, and **inspect logs** the same way you would operate real infrastructure.

---

## Why this exists

Most agent stacks today bury prompts, tool wiring, and permissions in application code. That makes it hard to answer basic operational questions: *Is this config valid? What changed? What are we about to deploy? What actually ran? Did policy allow it?*

**Agentic Control Plane** pushes those concerns into **explicit YAML** (Kubernetes-like resources) and a small **Go CLI** (`agentctl`), so teams can:

- Review diffs and plans before changes land  
- Track **deployment state** separately from **runtime traces**  
- Enforce **policies** (budgets, approvals, tool rules) at execution time  
- Stay **local-first** while the architecture leaves room for a future remote control plane  

The full product vision, YAML spec v0, and architecture are documented in [**`docs/DESIGN_DOC.md`**](docs/DESIGN_DOC.md).

---

## Mental model

| Idea | Analogy |
|------|---------|
| Desired resources in Git | **GitOps** |
| `plan` / `apply` / drift | **Terraform** |
| Typed resources (`Project`, `Workflow`, `Policy`, …) | **Kubernetes**-style API |
| Tool and IO contracts | **OpenAPI**-style explicitness |

---

## Features (MVP today)

- **`agentctl init`** — scaffold `project.yaml`, policies, tools, and a sample workflow  
- **`agentctl validate`** — load project, apply defaults/env overlays, validate graph, schemas, and references  
- **`agentctl plan`** — diff desired graph vs SQLite **deployment** state; risk hints  
- **`agentctl apply`** — persist plan (with TTY confirm or `--auto-approve` / `AGENTCTL_AUTO_APPROVE`)  
- **`agentctl run`** — execute a workflow locally; JSON Schema for inputs where configured; policy gates  
- **`agentctl logs`** — read **trace events** from SQLite (`--run`, `--workflow`, or recent runs)  
- **Output** — table, JSON, or YAML (`-o` / `--output`)  
- **State** — single SQLite file (default `.agentic/state.db` under the project root; override with `--state`)  
- **Tests** — unit/integration coverage, golden CLI output tests, end-to-end `init → … → logs` in `test/integration`  

See **section 18 (MVP)** and **section 19 (End Goal)** in [`docs/DESIGN_DOC.md`](docs/DESIGN_DOC.md) for the full included/excluded list.

---

## Quick start

### Prerequisites

- [Go 1.22+](https://go.dev/dl/)

### Build

```bash
git clone https://github.com/LAA-Software-Engineering/agentic-control-plane.git
cd agentic-control-plane
make build   # writes bin/agentctl
```

Or: `make install` / `go install ./cmd/agentctl` (honours `GOBIN` / `GOPATH/bin`; ensure that directory is on `PATH`).

### Create a project and run the loop

From the repo root (or anywhere):

```bash
agentctl init my-agent-system
agentctl validate --project my-agent-system
agentctl plan   --project my-agent-system
agentctl apply  --project my-agent-system --auto-approve
agentctl run    workflow/hello --project my-agent-system
agentctl logs   --project my-agent-system --workflow hello
```

### Example `project.yaml`

The project root is a **`Project`** resource: `apiVersion`, `kind`, `metadata.name`, and **`spec.imports`** listing other YAML files (policies, tools, workflows). After **`agentctl init my-agent-system`**, `my-agent-system/project.yaml` looks like this:

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
    model: openai/gpt-4o-mini
  providers:
    models:
      openai:
        type: openai
        apiKeyFrom: env:OPENAI_API_KEY
```

Field-by-field rules, extra kinds, and env overlays are in [`docs/DESIGN_DOC.md`](docs/DESIGN_DOC.md).

Notes:

- **`init`** creates `my-agent-system/` with `apiVersion: agentic.dev/v0` resources and a **`hello`** workflow (native `echo` tool only — **no network**).  
- **`apply`** in non-interactive environments needs **`--auto-approve`** or **`AGENTCTL_AUTO_APPROVE=1`**.  
- **`run`** stores traces in the **same** SQLite file used for plan/apply (default **`.agentic/state.db`** under `--project`).  
- Use **`logs --run <id>`** after a run if you want a single run’s trace (IDs are printed by **`run`**).  

### Global flags (common)

| Flag | Purpose |
|------|---------|
| `--project <path>` | Project root (default `.`) |
| `--state <path>` | SQLite file override |
| `-e` / `--env` | Environment overlay name |
| `-o` / `--output` | `table`, `json`, or `yaml` |
| `--no-color` | ASCII-friendly validate output |

Exit codes are summarized in **section 11.2** of [`docs/DESIGN_DOC.md`](docs/DESIGN_DOC.md) (`0` success, `2` validation, `4` execution, `5` policy denial, …).

---

## Repository layout (high level)

| Path | Role |
|------|------|
| `cmd/agentctl` | CLI entrypoint |
| `internal/cli` | Cobra commands, flags, golden tests |
| `internal/spec` | YAML types, normalize, validate |
| `internal/project` | Load project + imports |
| `internal/plan` | Planner and risk summary |
| `internal/apply` | Apply plan to deployment store |
| `internal/engine` | Workflow execution |
| `internal/policy` | Policy evaluation |
| `internal/state/sqlite` | SQLite deployment + runtime/trace tables |
| `test/integration` | End-to-end CLI flow tests |
| `docs/DESIGN_DOC.md` | Spec, CLI UX, architecture, roadmap |

---

## Development

**`make`** defaults to **`help`**, which lists targets; the table below mirrors the [`Makefile`](Makefile) (`##` comments and recipes).

| Target | What it does |
|--------|----------------|
| `help` | Show usage and target list (default goal) |
| `all` | `fmt` → `vet` → `test` → `build` (handy before a push) |
| `build` | `go build` → `bin/agentctl` |
| `install` | `go install ./cmd/agentctl` (`-trimpath`; uses `GOBIN` / `GOPATH/bin`) |
| `clean` | Remove `bin/` and `coverage.out` |
| `fmt` | `go fmt ./...` |
| `verify-fmt` | Fail if `gofmt -l` would list files (matches CI-style formatting check) |
| `vet` | `go vet ./...` |
| `test` | `go test ./... -race` |
| `test-coverage` | Tests with `-coverprofile=coverage.out` and a one-line `go tool cover -func` summary |
| `check` | `vet` + `test` only (no formatting writes) |
| `ci` | `verify-fmt` + `vet` + `test` (no build) |

CI (`.github/workflows/ci.yml`) runs **Linux, macOS, and Windows** on Go **1.22.x**, plus **Go 1.23.x** on Linux, with **race** and **shuffle** enabled (workflow steps are defined in YAML, not via `make ci`).

### Updating CLI golden files

When table output is intentionally changed:

```bash
GO_UPDATE_GOLDEN=1 go test ./internal/cli/... -run TestGolden_
```

---

## Roadmap

### Near term (MVP hardening)

- More **`diff` / drift** UX where the design doc calls for it  
- **`inspect`** and richer **`logs`** filters (see sections **10.2** and **17.3** in `docs/DESIGN_DOC.md`)  
- **`agentctl test`**-style workflow fixtures (**stretch** per design doc)  

### Post-MVP (from design doc section 19)

- Modules/registry, remote shared state, reconciliation controllers  
- Parallel steps, subworkflows, schedules/events  
- Stronger drift semantics and multi-runtime targets  
- Deeper approval workflows and multi-tenant controls  

The **recommended implementation phases** are outlined in **section 20** of [`docs/DESIGN_DOC.md`](docs/DESIGN_DOC.md).

---

## Documentation

- **[`docs/DESIGN_DOC.md`](docs/DESIGN_DOC.md)** — design document v0 (problem statement, spec, CLI, engine, state model, testing strategy, MVP vs end state, section 23 recommendation).  
- **[`docs/EXAMPLES.md`](docs/EXAMPLES.md)** — copy-paste YAML and CLI examples (`init`, mock vs OpenAI, workflows, environment overlays).  
- **[`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md)** — Contributor Covenant 2.1; participation expectations and reporting.  
- **License:** [MIT](LICENSE)  

---

## Contributing

Issues and pull requests are welcome. See **[`CONTRIBUTING.md`](CONTRIBUTING.md)** for local setup, tests, golden updates, and pull request expectations.

---

> **Local declarative agent systems with validate, plan, apply, run, and logs.**  
> *(Closing recommendation in [`docs/DESIGN_DOC.md`](docs/DESIGN_DOC.md), section 23.)*
