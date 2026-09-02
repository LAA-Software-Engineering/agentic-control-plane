# Security Policy

Terfyn's purpose is a security property: make the authority of nondeterministic agents **statically
bounded, reviewable before execution, and invariant across the run**. So we treat certain bugs as
security issues even when nothing crashes — see "What counts as a security issue" below.

## Supported versions

Terfyn is pre-1.0 and releases frequently. Security fixes land on **`main`** and the **latest
`0.x` release**. Older `0.x` releases are not separately patched — please upgrade to the latest
release before reporting, and confirm the issue still reproduces there or on `main`.

| Version            | Supported |
|--------------------|-----------|
| latest `0.x` release / `main` | ✅ |
| any older release  | ❌ (upgrade first) |

## Reporting a vulnerability

**Please do not open a public issue for a vulnerability, and do not include a working exploit in any
public thread.**

Report privately through GitHub's **private vulnerability reporting**:

1. Go to the repository's **Security** tab → **Report a vulnerability**
   (<https://github.com/Terfyn/terfyn/security/advisories/new>).
2. Describe the issue, the impact, and how to reproduce it.

This opens a private advisory visible only to you and the maintainers. If private reporting is
unavailable to you, open a public issue that says only "requesting a security contact" — with **no
technical detail** — and a maintainer will follow up with a private channel.

A good report includes:

- affected version (`terfyn version`) and platform;
- the impact — what an attacker gains, and the trust boundary crossed;
- a **minimal** reproduction: the smallest `.agent`/YAML project and the exact commands;
- which soundness invariant is broken, if you can identify it (see below).

## What counts as a security issue

The highest-severity class is not a crash — it is any way the **authority Terfyn reviews before a
run diverges from the authority actually available during the run.** Concretely, a break of any
invariant in [`docs/SOUNDNESS.md`](docs/SOUNDNESS.md) (S1–S9) is a security issue, for example:

- a run exercising authority that `terfyn plan` did not surface (an unreported widening);
- a closed-world tool's callable set growing at dispatch beyond its deployed manifest;
- a pinned/resumed run reading mutable current config, policy, or schemas;
- an external runtime (`--runtime claude-code`) reaching a built-in tool or any operation outside
  the compiled grants (S9);
- a policy / `CheckToolCall` / approval (HITL) / budget check that fails **open**;
- tenant, thread, or actor isolation breaking, or secrets/redaction leaking into traces or output.

Also in scope: memory-safety or injection bugs, credential/secret exposure, and denial of service in
the CLI or engine.

**Out of scope:** issues in a model provider or a third-party MCP/HTTP tool you configured (report
those upstream); the *documented* open-world carve-out for tools that declare no `operations:` (that
is opt-out, not a vulnerability — see S2); and the fact that a *granted* broad capability is broad —
Terfyn's contract is that broad authority is **visible** in `plan`, not that it is forbidden.

## Disclosure

We aim to acknowledge a report within a few days and to fix confirmed issues promptly, coordinating
a disclosure timeline with you. Please give us a reasonable window to release a fix before any public
disclosure. We're happy to credit reporters in the advisory unless you prefer to remain anonymous.
