# implement-review-loop — bounded control around nondeterministic agents

This is not merely an LLM workflow. It is:

```
              deterministic bounded control
                        around
                 nondeterministic agents
```

An **Implementer** and an independent **Reviewer** pass a structured `CodingState`
back and forth inside a bounded loop — at most **three** implement/review rounds.
The agents are free to behave nondeterministically; Terfyn statically bounds the
authority each may exercise, how many iterations may run, and which effect classes
are reachable — all reviewable as a `plan` diff before anything runs.

```
                  max 3 iterations  (source: `while … limit 3`)
                        |
                        v
                   Implementer   ── may: read_file, write_file, run_tests
                        |
                        v
                    Reviewer     ── may: read_file, run_tests   (NOT write_file)
                   /         \
            approved          rejected
               |                 |
               v                 |
             return <------------+
```

Approved on the first review exits immediately. Never approved stops after the
**third** review and returns the final state — there is no silent fourth attempt.

## The program

Everything computational is authored in [`main.agent`](main.agent): both agents
(with their real prompts in `instructions`, typed `CodingState` input/output, and
their capability grants) and the workflow:

```agent
workflow ImplementAndReview(input: CodingState) -> CodingState
    effects { workspace.read, workspace.write, process.exec }
{
    state = input

    while !state.approved limit 3 {
        implementation = Implementer(state)
        state = Reviewer(implementation)
    }

    return state
}
```

Only resource *configuration* is YAML: the [`workspace` tool](tools/workspace.yaml)
and its per-operation effects, the two [policies](policies/), the project config,
and the [`CodingState` schema](schemas/CodingState.json). Agent prompts live in
`.agent`, not YAML.

## The capability boundary is the security control — not the prompt

The Reviewer's prompt says *"You must not modify the workspace."* **That sentence is
not the boundary.** The boundary is the grant it does **not** hold:

```
Reviewer instructions:     "do not edit the workspace"
Reviewer authority:        tool.workspace.read_file, tool.workspace.run_tests
Reviewer lacks:            tool.workspace.write_file
```

A Reviewer that — hallucinating, prompt-injected, or simply mistaken — attempts
`workspace.write_file(...)` is **denied because the operation is outside its declared
capability**, not because the prompt asked it not to. The Implementer holds
`write_file` and may use it. The capability system wins even when the prompt does
not. (Enforced at the tool-resolution boundary — see
`TestImplementReviewLoop_ReviewerCannotWrite` in
[`internal/engine/implement_review_test.go`](../../internal/engine/implement_review_test.go).)

## What is bounded vs. what is free

| The agents may nondeterministically… | Terfyn statically bounds… |
|---|---|
| choose which files to inspect | which concrete operations each agent may invoke |
| choose which *permitted* tools to call | which effect classes are reachable (`workspace.read`, `workspace.write`, `process.exec`) |
| produce different code each run | how many implement/review iterations run (**≤ 3**) |
| produce different review feedback | where human approval may be required |
| approve or reject | what deployment authority changed (reviewable in `plan`) |

The iteration bound and the effect bound are **separate axes**: `limit 3` bounds how
many times the loop runs; the effect set is the union of the body's reachable
effects — `{workspace.read, workspace.write, process.exec}` — independent of the
iteration count. Terfyn does not multiply effects by iterations.

## Inspect it offline (no API keys)

`validate` / `plan` / `apply` run on the deterministic `mock/gpt-4` model, so the
capability boundary, the authority diff, and every bound are reproducible with no keys:

```bash
terfyn validate --project examples/implement-review-loop
terfyn plan     --project examples/implement-review-loop
terfyn apply    --project examples/implement-review-loop --auto-approve
```

`plan` prints the effect bound — every effect each agent may autonomously reach,
with the tool operation that reaches it:

```text
Effect bound (Agent/Reviewer):
- [high] effect_bound: process.exec     autonomous  Agent/Reviewer may select tool.workspace.run_tests
- [high] effect_bound: workspace.read   autonomous  Agent/Reviewer may select tool.workspace.read_file
```

The Reviewer's bound has **no `workspace.write`** — it cannot reach that effect,
because it was never granted `write_file`.

## Run it for real

The `workspace` tool ([`tools/workspace.yaml`](tools/workspace.yaml)) is the native
filesystem + test-runner adapter: `read_file` / `write_file` operate inside a **sandbox
root**, and `run_tests` runs an **operator-configured** command in it. Executing the loop
therefore needs a real model to drive the agents and two env vars for the sandbox:

```bash
export ANTHROPIC_API_KEY=…                            # or OPENAI_API_KEY, matching the provider block
export TERFYN_WORKSPACE_ROOT="$(mktemp -d)"           # the sandbox the agents read/write within
export TERFYN_WORKSPACE_TEST_COMMAND="go test ./..."  # what run_tests executes, in the root

# point the agents at a real model: edit defaults.model / the agents' model to anthropic/… or openai/…

terfyn run workflow/ImplementAndReview \
  --project examples/implement-review-loop \
  --input-file examples/implement-review-loop/fixtures/task.json
```

The sandbox root confines every `read_file` / `write_file` — a `..` path or symlink that would
escape is rejected — and `run_tests` runs only the configured command, never a command the agent
chooses, so the capability boundary holds at the filesystem too.

Instead of the env vars, the sandbox root and test command can be declared on the tool
(`spec.workspace.root` / `testCommand` in [`tools/workspace.yaml`](tools/workspace.yaml)); a
relative root resolves against the project root, and declared config takes precedence over the env.

> The deterministic `mock/gpt-4` model drives `validate` / `plan` / `apply`, but it cannot
> execute the tool loop — it emits empty-argument tool calls — so `run` needs a real model.
> The loop semantics, the capability boundary, and every bound are identical either way.

## Authority changes are reviewable before deployment

Suppose someone widens the Reviewer to also write — a one-line grant change:

```diff
  grants {
      tool.workspace.read_file
      tool.workspace.run_tests
+     tool.workspace.write_file
  }
```

`terfyn plan` does **not** treat this as a changed config string. It surfaces it as
an **autonomous authority widening** — a nondeterministic agent's action space grew:

```text
~ update Agent/Reviewer
    spec.tools.2:  -> "tool.workspace.write_file"

Effect bound (Agent/Reviewer):
- [high] effect_bound: workspace.write   autonomous  Agent/Reviewer may select tool.workspace.write_file

Capability delta:
Agent/Reviewer
+ tool.workspace.write_file

Authority:
  autonomous  -> WIDENED

Risk delta:
high:
- [high] authority_widening: AUTONOMOUS authority WIDENED.
```

Before the change the Reviewer *cannot* write; after it, the Reviewer *autonomously
may*. That difference is a reviewable, high-severity line in the plan — visible to a
human before `apply`, not discovered in production.

## The statement this example makes concretely true

> The model may behave nondeterministically, but Terfyn defines the maximum authority
> it can exercise, the maximum number of bounded control-flow iterations around it,
> makes authority changes reviewable before deployment, and durably resumes execution
> without duplicating completed side effects.

Everything here runs on the execution IR (`execir`) — the bounded `while`, the
capability gating, and durable resume across iterations (a HITL pause between
Implementer and Reviewer resumes at the correct iteration without re-issuing a
completed effect). Nothing depends on the retired `WorkflowStep` DAG runtime.
