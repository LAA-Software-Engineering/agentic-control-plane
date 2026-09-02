# external-runtime-reviewer

The flagship proof for the external agent-runtime epic ([#335](https://github.com/Terfyn/terfyn/issues/335)):
the **same** reviewed `.agent` program runs under Terfyn's own engine **or** an external CLI
agent, and the authority is identical either way. The runtime is replaceable; the authority is not.

A read-only `Reviewer` is granted exactly two workspace operations — `read_file` and `run_tests`.
The `workspace` tool also declares `write_file`, but the Reviewer is **not** granted it. Under the
external runtime the grant compiles into a per-run Terfyn MCP server whose `tools/list` is exactly
`{ read_file, run_tests }`; `write_file` — a real operation on the tool — is never advertised, so
the external model **literally cannot select it**.

Everything below runs offline on the mock model — no external process, no API key.

## 1. What the program can do — `terfyn plan`

```bash
terfyn plan --project examples/external-runtime-reviewer --state /tmp/rev.db
```

The effect bound is computed from the grants against the tool's closed manifest. The Reviewer's
two grants become reachable effects; `write_file` is reported **unreachable**:

```text
Effect bound (Agent/Reviewer):
high:
- [high] effect_bound: process.exec       autonomous  Agent/Reviewer may select tool.workspace.run_tests
- [high] effect_bound: workspace.read     autonomous  Agent/Reviewer may select tool.workspace.read_file
low:
- [low] effect_bound: workspace.write    unreachable no grant path to tool.workspace.write_file

Runtime targets:
  Workflow/review -> local
```

That last effect-bound line is the claim in plan form: **there is no grant path to
`tool.workspace.write_file`.** The `Runtime targets` section shows which runtime will execute the
workflow — swapping it does not change any line above it, because the bound comes from the graph
alone.

## 2. Prove it — `terfyn test`

The capability assertion in [`tests/capabilities.yaml`](tests/capabilities.yaml) fails the build if
the Reviewer could ever produce `workspace.write`:

```bash
terfyn test --project examples/external-runtime-reviewer
```

```text
tests/capabilities.yaml   forbid Reviewer → workspace.write  pass
1 passed, 0 failed
```

This is the reproducible form of "the model literally cannot select the operation": the assertion is
a static property of the reviewed program, checked before anything runs, and it holds regardless of
which runtime executes it.

## 3. The same program under `--runtime claude-code`

```bash
terfyn plan  --project examples/external-runtime-reviewer --state /tmp/rev.db
terfyn apply --project examples/external-runtime-reviewer --state /tmp/rev.db --auto-approve
terfyn run   workflow/review --project examples/external-runtime-reviewer --state /tmp/rev.db \
    --runtime claude-code --input change="add a null check"
```

`--runtime claude-code` selects the external adapter instead of the built-in engine for this run.
The `.agent`, the grants, the policy, and the effect bound are **unchanged** — only the execution
substrate differs. When the run executes, the grant compiles to the per-run MCP server described
above (its `tools/list` is exactly `read_file` and `run_tests`), every tool call still passes
Terfyn `CheckToolCall` / policy / HITL, and the turns and tool calls fold into the same hash-linked
audit chain as a local run.

> **Status.** The adapter and the grant → MCP-server compiler are in place; the final step —
> wiring a live `claude -p` run through them — is the last integration of the epic, so
> `terfyn run --runtime claude-code` currently **fails closed** with a clear not-yet-wired error
> rather than silently doing nothing. The authority claim above is already reproducible today via
> `plan` and `test`, which is the point: the boundary is a property of the reviewed program, not of
> the harness that runs it.

## Why this is sound

`write_file` is a real capability of the `workspace` tool — it is *declared*, just not *granted*.
That distinction is the whole game:

- Terfyn never maps a scoped grant onto an external built-in tool (no `--tools "Bash"`); an external
  agent gets **no** built-in tools, only the granted operations served as MCP ops. This is invariant
  **S9** in [`docs/SOUNDNESS.md`](../../docs/SOUNDNESS.md).
- Broad capability is allowed, but only *loudly*: it would show up in `terfyn plan` as reachable
  authority. A hidden broad capability is the non-goal ([ADR 004 §5](../../docs/adr/004-scope-and-non-goals.md)).

See [`docs/EXTERNAL_RUNTIME.md`](../../docs/EXTERNAL_RUNTIME.md) for the AgentRuntime boundary and
the grant-is-not-a-builtin rule, and [ADR 006](../../docs/adr/006-external-agent-runtimes.md) for the
decision to promote `RuntimeTarget` to a real resource.
