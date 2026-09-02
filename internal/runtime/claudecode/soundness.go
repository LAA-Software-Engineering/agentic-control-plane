package claudecode

import (
	"fmt"
	"strings"
)

// Soundness guard for the external Claude Code runtime (issue #339, docs/SOUNDNESS.md S9).
//
// S9 — the external-runtime callable set is exactly the pinned grant operations served by
// Terfyn's per-run MCP server; the spawned agent gets NO built-in tools. Mapping a scoped grant
// onto a built-in (e.g. tool.workspace.run_tests -> Bash) is unsound: `terfyn plan` would
// advertise `effect bound: workspace.test` while the real authority is Bash's unbounded
// filesystem/network/process/git. Broad capability is allowed, but only *loudly*: arbitrary
// shell must be an explicit `grants { tool.shell.exec }` that compiles (via internal/runtime/
// mcpserver) to a Terfyn-owned MCP op, so plan shows the true authority surface (ADR 004 §5).
//
// The adapter never emits a built-in-tool allowance itself (see argv / denyBuiltinToolsArgs).
// This guard is the fail-closed fence for the one remaining ingress — caller-supplied
// RunSpec.ExtraArgs. Two checks cover the two ways an ExtraArgs flag can break S9:
// checkNoBuiltinToolExposure rejects anything that would expose a built-in tool or bypass the
// permission boundary, and checkExtraArgsNoAuthoritySurface rejects the transport/scope flags
// (--mcp-config, --add-dir) that would alter the callable set out of band from the grant.

// ErrUnsoundToolExposure is returned when an assembled argv would expose a built-in tool or
// bypass the permission boundary, in violation of S9. It is a hard refusal, not a warning: the
// process is never spawned.
type unsoundToolExposureError struct{ reason string }

func (e *unsoundToolExposureError) Error() string {
	return "claudecode: unsound tool exposure (SOUNDNESS.md S9): " + e.reason
}

// checkNoBuiltinToolExposure scans an assembled argv and refuses any flag that would grant the
// external agent a built-in tool or bypass its permission boundary. In an allow-position flag
// (--tools / --allowedTools), only Terfyn's own MCP tools (the "mcp__" namespace) are sound; a
// bare built-in name (Bash, Edit, Read, …) is not. Restricting flags (--disallowedTools) and the
// empty denial value (--tools "") are left alone.
func checkNoBuiltinToolExposure(argv []string) error {
	for i := 0; i < len(argv); i++ {
		flag, inlineVal, hasInline := splitFlag(argv[i])
		switch normalizeFlag(flag) {
		case "--dangerously-skip-permissions":
			return &unsoundToolExposureError{reason: "--dangerously-skip-permissions removes the permission boundary the runtime relies on"}
		case "--permission-mode":
			val := inlineVal
			if !hasInline {
				val = next(argv, i)
			}
			if strings.EqualFold(strings.TrimSpace(val), "bypassPermissions") {
				return &unsoundToolExposureError{reason: "--permission-mode bypassPermissions removes the permission boundary"}
			}
		case "--tools", "--allowedtools", "--allowed-tools":
			val := inlineVal
			if !hasInline {
				val = next(argv, i)
			}
			if tok, ok := firstBuiltinToken(val); ok {
				return &unsoundToolExposureError{reason: fmt.Sprintf("%s would expose built-in tool %q; grants must resolve to Terfyn MCP ops (mcp__*), not built-ins", flag, tok)}
			}
		}
	}
	return nil
}

// checkExtraArgsNoAuthoritySurface rejects the authority-surface flags that only the adapter may
// set, if they arrive via caller-supplied ExtraArgs. These do not expose a built-in tool, but
// they still change "the callable set is exactly the pinned grants" (S9), so the built-in fence
// alone is not enough:
//
//   - --mcp-config: the per-run MCP server is set from RunSpec.MCPConfig. A second --mcp-config
//     smuggled through ExtraArgs registers a server whose tools the CLI calls directly — they
//     never pass the mcpserver PolicyDispatcher / CheckToolCall path, so their operations are
//     outside the pinned grants entirely.
//   - --add-dir: widens the filesystem scope the agent can reach, out of band from the grant.
//
// ExtraArgs is Terfyn-internal, not agent-controlled, so this is defense in depth: the ingress
// must carry neither transport nor scope flags. It is checked on ExtraArgs specifically (not the
// whole argv) because the adapter legitimately emits its own single --mcp-config.
func checkExtraArgsNoAuthoritySurface(extraArgs []string) error {
	for _, arg := range extraArgs {
		flag, _, _ := splitFlag(arg)
		switch normalizeFlag(flag) {
		case "--mcp-config":
			return &unsoundToolExposureError{reason: "--mcp-config must not be passed via ExtraArgs: the per-run MCP server is set from RunSpec.MCPConfig, and a smuggled server's tools would bypass Terfyn policy (CheckToolCall)"}
		case "--add-dir":
			return &unsoundToolExposureError{reason: "--add-dir must not be passed via ExtraArgs: it widens the agent's filesystem authority surface out of band from the grant"}
		}
	}
	return nil
}

// firstBuiltinToken reports the first token in an allow-position value that is not a Terfyn MCP
// tool reference. An empty value (the denial, --tools "") has no tokens and is sound.
func firstBuiltinToken(val string) (string, bool) {
	for _, tok := range splitTokens(val) {
		if tok == "" {
			continue
		}
		if !strings.HasPrefix(tok, "mcp__") {
			return tok, true
		}
	}
	return "", false
}

// splitFlag splits "--flag=value" into ("--flag", "value", true); a bare "--flag" yields
// ("--flag", "", false).
func splitFlag(arg string) (flag, val string, hasVal bool) {
	if i := strings.IndexByte(arg, '='); i >= 0 && strings.HasPrefix(arg, "-") {
		return arg[:i], arg[i+1:], true
	}
	return arg, "", false
}

func normalizeFlag(flag string) string { return strings.ToLower(strings.TrimSpace(flag)) }

func next(argv []string, i int) string {
	if i+1 < len(argv) {
		return argv[i+1]
	}
	return ""
}

// splitTokens splits a tool list on commas and whitespace (Claude Code accepts either).
func splitTokens(val string) []string {
	return strings.FieldsFunc(val, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
}
