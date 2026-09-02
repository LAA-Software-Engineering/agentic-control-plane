# Terfyn Agent — VS Code extension

Syntax highlighting for Terfyn [`.agent`](../../docs/LANGUAGE.md) authoring files: the surface
syntax for agents and workflows (ADR 002/003).

`.agent` has a real, formal grammar — top-level `agent` / `workflow` / `tool` / `policy`
declarations, control flow (`if`/`else`, `for`/`in`, `while`/`limit`, `retry`/`until`, `parallel`,
`return`), capability `grants` and `effects`, model references, `${…}` string interpolation, and
multiline `"""…"""` prose. This extension makes it read like a language instead of a config blob.

The grammar ([`syntaxes/agent.tmLanguage.json`](syntaxes/agent.tmLanguage.json)) is a TextMate
grammar derived from `docs/LANGUAGE.md`. TextMate is the format GitHub Linguist consumes, so this is
also the on-ramp for native `.agent` highlighting on GitHub later (issue #362, Stage 2).

## Install without the Marketplace

Build the `.vsix` and install it:

```bash
cd editors/vscode
npm install
npm run package          # → terfyn-agent-<version>.vsix
code --install-extension terfyn-agent-*.vsix
```

CI builds the `.vsix` on every PR that touches `editors/vscode/` (see
`.github/workflows/vscode-extension.yml`); a Marketplace publish (`vsce publish`) is a later,
token-gated step.

## Scope

Authoring/DX tooling around the existing language. It does not touch runtime authority or the
closed-world model.
