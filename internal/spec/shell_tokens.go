package spec

import "strings"

var shellReadOnlyTokens = map[string]struct{}{
	"ls": {}, "cat": {}, "grep": {}, "head": {}, "tail": {}, "stat": {},
	"find": {}, "pwd": {}, "wc": {}, "which": {},
}

// ShellTokenClass classifies the first token of a shell command for shell_safe policy.
type ShellTokenClass int

const (
	ShellTokenUnknown ShellTokenClass = iota
	ShellTokenReadOnly
	ShellTokenGate
)

// ClassifyShellToken maps the first command token to read-only, gate, or unknown (fail-closed → gate).
func ClassifyShellToken(token string) ShellTokenClass {
	token = normalizeShellToken(token)
	if token == "" {
		return ShellTokenUnknown
	}
	if _, ok := shellReadOnlyTokens[token]; ok {
		return ShellTokenReadOnly
	}
	if _, ok := shellGateTokens[token]; ok {
		return ShellTokenGate
	}
	return ShellTokenUnknown
}

func normalizeShellToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	token = strings.Trim(token, `"'`)
	if i := strings.LastIndexAny(token, "/\\"); i >= 0 {
		token = token[i+1:]
	}
	if idx := strings.IndexByte(token, '='); idx >= 0 {
		token = token[:idx]
	}
	return strings.ToLower(strings.TrimSpace(token))
}

// FirstShellToken returns the first whitespace-delimited token from a shell command string.
func FirstShellToken(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], `"'`)
}

// ExtractShellCommand reads a command string from a workflow step input map.
func ExtractShellCommand(with map[string]any) string {
	if with == nil {
		return ""
	}
	for _, key := range []string{"command", "cmd", "script"} {
		if v, ok := with[key]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}
