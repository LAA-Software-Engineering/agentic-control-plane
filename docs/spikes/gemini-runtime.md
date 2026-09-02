# Spike: Gemini CLI as a Terfyn external runtime

**Date:** 2026-09-02 · **Epic:** [#409](https://github.com/Terfyn/terfyn/issues/409) · **Contract:**
[`RUNTIME_TARGET_CONTRACT.md`](../RUNTIME_TARGET_CONTRACT.md)

**Verdict: viable candidate — recommended second adapter.** Unlike Codex, Gemini CLI can be fenced to
**MCP-only**: `coreTools` (migrating to `tools.core`) is a strict allowlist for *all* built-in tools, so
an empty allowlist disables every built-in (shell, file, web), while **MCP tools live in a separate
registry** (`mcpServers`) unaffected by it. That is exactly the R3 mechanism Codex lacks. Meets R1–R6
in principle, pending a live S9 verification against a pinned version.

> Documentation-only research (Sept 2026); no binary was run. The R3 mechanism must be confirmed live
> against a pinned Gemini version before the runtime serves a live run (see below).

## Contract scorecard

| Req | Status | Notes |
|---|---|---|
| R1 non-interactive | ✅ | `gemini -p "<prompt>"` runs headless (non-TTY or `-p`). |
| R2 MCP injection + closed world | ✅ | `mcpServers.terfyn.httpUrl` + `allowMCPServers: ["terfyn"]` closes the world to just Terfyn's server; per-server `includeTools` can pin further. |
| **R3 built-in lockdown + live S9** | ✅ mechanism exists | Empty `coreTools`/`tools.core` (strict allowlist) disables **all** built-in tools; MCP tools are a separate registry and remain available. Confirmed behaviorally by issue [#18807](https://github.com/google-gemini/gemini-cli/issues/18807). **Still requires the gated live S9 test** to prove empty-core truly fences shell/file/web against the pinned version. |
| R4 structured output → `Session` | ✅ | Headless returns structured text or JSON (`--output-format json`). |
| R5 budget / turn / timeout | ⚠️ partial | Per-server `timeout`; turn/cost caps less explicit — acceptable, Terfyn's `EnforceBudget` stays enforcer of record. |
| R6 authenticated loopback | ✅ | `mcpServers.<n>.headers` sends `Authorization: Bearer …` (also OAuth for SSE/HTTP); maps onto `ListenLocal`'s token. |

## Seams / caveats to pin before building

- **Schema migration:** `coreTools` → nested `tools.core` (PR #27947). Pin the Gemini version and use
  the key that version expects.
- **R3 is behavior, not a documented guarantee.** "Empty allowlist disables all built-ins" is reported
  via issue #18807 (and #14482 asks for a simpler boolean). The live S9 test is therefore mandatory —
  it is the whole point of R3.
- **Isolated settings.** Gemini merges system/workspace/user `settings.json`; `coreTools` intersects and
  `excludeTools` unions across levels. The adapter must run Gemini against a Terfyn-controlled settings
  dir/env so ambient user settings cannot re-enable a built-in or add an MCP server. Confirm the env/flag
  that pins the settings source.
- **Closed world:** set `allowMCPServers` to exactly `["terfyn"]` so no ambient MCP servers leak in.

## Recommendation

Make **Gemini CLI the second adapter** (`internal/runtime/gemini`) implementing the contract, starting
with the pinned-version confirmation of the R3 mechanism + its gated live S9 test. Building it will also
force the interface refactor the contract calls out (lifting the `stream-json` assumption in `Session`
and the Claude flag names in `RunSpec`), since Gemini's invocation and output differ from Claude's.

## Sources

- [Gemini CLI headless mode](https://geminicli.com/docs/cli/headless/)
- [MCP servers with the Gemini CLI (httpUrl + headers, allowMCPServers, includeTools/excludeTools)](https://github.com/google-gemini/gemini-cli/blob/main/docs/tools/mcp-server.md)
- [google-gemini/gemini-cli #18807 — `tools.core` allowlist disables other built-ins](https://github.com/google-gemini/gemini-cli/issues/18807)
- [google-gemini/gemini-cli #14482 — request to disable local file/command tools](https://github.com/google-gemini/gemini-cli/issues/14482)
