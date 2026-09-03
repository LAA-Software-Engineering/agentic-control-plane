package gemini

import "strings"

// Soundness guard for the Gemini runtime (docs/SOUNDNESS.md S9). The built-in-tool lockdown and the
// per-run MCP server are carried by the workspace settings.json this adapter writes; the one ingress
// that could subvert them is the Terfyn-internal RunSpec.ExtraArgs. checkExtraArgsNoAuthoritySurface
// fails closed before spawning if ExtraArgs carries a flag that would re-enable built-in tools,
// register another MCP server, widen the filesystem/settings scope, or drop the sandbox — any of
// which would make the grant-compiled MCP server no longer the only authority surface.
//
// ExtraArgs is not agent-controlled, so this is defense in depth. The flag set is the spike's best
// reading of the Gemini CLI and must be reconciled with a pinned `gemini --help`.
type unsoundExtraArgsError struct{ reason string }

func (e *unsoundExtraArgsError) Error() string {
	return "gemini: unsound ExtraArgs (SOUNDNESS.md S9): " + e.reason
}

func checkExtraArgsNoAuthoritySurface(extraArgs []string) error {
	for _, arg := range extraArgs {
		flag, _, _ := splitFlag(arg)
		switch normalizeFlag(flag) {
		case "--yolo", "-y":
			return &unsoundExtraArgsError{reason: "--yolo auto-approves every tool call, removing the permission boundary"}
		case "--approval-mode":
			return &unsoundExtraArgsError{reason: "--approval-mode must not be set via ExtraArgs: approvals are Terfyn's HITL at the MCP dispatch layer"}
		case "--allowed-tools", "--allowedtools", "--core-tools", "--coretools":
			return &unsoundExtraArgsError{reason: "tool-allowlist flags would re-enable built-in tools out of band from the workspace tools.core lockdown"}
		case "--mcp-server", "--mcpservers", "--extensions", "-e":
			return &unsoundExtraArgsError{reason: "registering another MCP server / extension would add tools that never pass Terfyn CheckToolCall"}
		case "--include-directories", "--include-dir", "--add-dir":
			return &unsoundExtraArgsError{reason: "widening the filesystem scope is out of band from the grant"}
		case "--sandbox", "-s":
			return &unsoundExtraArgsError{reason: "--sandbox must not be overridden via ExtraArgs"}
		}
	}
	return nil
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
