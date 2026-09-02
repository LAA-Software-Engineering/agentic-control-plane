# Spike: Grok Build CLI as a Terfyn external runtime

**Date:** 2026-09-02 · **Epic:** [#409](https://github.com/Terfyn/terfyn/issues/409) · **Contract:**
[`RUNTIME_TARGET_CONTRACT.md`](../RUNTIME_TARGET_CONTRACT.md)

**Verdict: conditionally viable — a middle ground, behind Gemini.** xAI's `grok -p` (Grok Build) has
HTTP MCP with auth headers, headless streaming-json, and *both* allow/deny flags for built-in tools —
so unlike Codex it has an R3 mechanism. The catch: the closed-world path (an empty `--tools` allowlist)
is reported to fail because `run_terminal_cmd` is treated as **required**, and the working fallback
(`--disallowed-tools`) is a *blocklist*, not a closed world. Whether Grok is sound hinges on one thing
that must be confirmed live: can a pinned build fully remove the shell via the allowlist?

> Documentation-only research (Sept 2026); no binary was run. Some R3 details come from a community
> plugin, not xAI docs, so they especially need pinned-version confirmation.

## Contract scorecard

| Req | Status | Notes |
|---|---|---|
| R1 non-interactive | ✅ | `grok -p "<prompt>"` runs headless; fresh session per call (`-r`/`-c` to resume). |
| R2 MCP injection + closed world | ✅ transport / ⚠️ closed world | HTTP MCP via `[mcp_servers.x] url + headers` in `~/.grok/config.toml`; tools namespaced `<server>__<tool>`. Closed world depends on R3 (removing built-ins) + controlling the config source so no ambient servers load. |
| **R3 built-in lockdown + live S9** | ⚠️ **conditional** | Allow/deny flags exist: `--tools <allowlist>` (built-ins; **MCP meta-tools always remain**) and `--disallowed-tools <denylist>` (e.g. `run_terminal_cmd`). **But** the allowlist path — the closed-world one — is reported to break session creation with "Requirements unsatisfied for `run_terminal_cmd`", i.e. the shell is treated as required; the documented workaround is the denylist, which is a blocklist (must enumerate every built-in; a new built-in in a later version leaks). **Blocker unless** a pinned build lets an empty/minimal allowlist fully remove the shell. Must be proven by the gated live S9 test. |
| R4 structured output → `Session` | ✅ | `--output-format streaming-json` (files modified, commands run, results). Read-only stream; bidirectional approvals use ACP — not needed, since Terfyn's HITL runs at the MCP-dispatch layer, as with Claude. |
| R5 budget / turn / timeout | ⚠️ partial | Sandbox/network boundaries + permissions; explicit turn/cost caps unclear — acceptable, Terfyn's `EnforceBudget` stays enforcer of record. |
| R6 authenticated loopback | ✅ | `headers = { "Authorization" = "Bearer ${TOKEN}" }` with `${VAR}` expansion; `grok mcp add --transport http … --header`. Maps onto `ListenLocal`'s token. |

## The R3 question that decides it

Terfyn needs the grant-compiled MCP server to be the **only** authority surface (S9). Grok's MCP
meta-tools surviving the allowlist is exactly right — that is the surface we want. The open question is
the shell:

- **If** an empty `--tools ""` allowlist reliably removes `run_terminal_cmd` and the other built-ins on
  the pinned build → Grok is closed-world and **viable** (comparable to Gemini).
- **If** `run_terminal_cmd` cannot be removed (as the 0.2.x bug implies) → Grok always carries a built-in
  shell off the `CheckToolCall`/HITL/trace path → **blocked on R3, like Codex.**

The denylist (`--disallowed-tools`) is not a substitute: a blocklist that must name every built-in is not
a closed world and will silently reopen the moment Grok ships a new built-in.

## Recommendation

Rank behind Gemini. Keep Grok Build as the **backup second adapter**: before any code, run the pinned
allowlist confirmation (does empty `--tools` fully fence built-ins?) + the gated live S9 test. Pursue it
only if that confirms; otherwise it parks next to Codex until the allowlist path is fixed
(`GROK_CC_FORCE_TOOLS_ALLOWLIST=1` hints a fix is in progress).

## Sources

- [Grok Build headless mode (`grok -p`, `--tools`, `--disallowed-tools`, streaming-json)](https://github.com/xai-org/grok-build/blob/main/crates/codegen/xai-grok-pager/docs/user-guide/14-headless-mode.md)
- [Grok Build MCP servers (config.toml url + headers)](https://docs.x.ai/build/features/mcp-servers)
- [Grok Build permissions & safety](https://github.com/xai-org/grok-build/blob/main/crates/codegen/xai-grok-pager/docs/user-guide/22-permissions-and-safety.md)
- [Community plugin note — `--tools` allowlist / `run_terminal_cmd` session-creation bug](https://github.com/VasiHemanth/grok-build-plugin/blob/main/README.md)
