package spec

import "strings"

// Shell command operation names for native tools (single source of truth, issue #104).
var ShellCommandOperations = []string{"command.run", "run", "exec", "shell"}

var shellReadOnlyTokens = map[string]struct{}{
	"ls": {}, "cat": {}, "grep": {}, "head": {}, "tail": {}, "stat": {},
	"find": {}, "pwd": {}, "wc": {}, "which": {},
}

var shellGateTokens = map[string]struct{}{
	"rm": {}, "mv": {}, "cp": {}, "chmod": {}, "chown": {}, "mkfifo": {}, "dd": {},
	"curl": {}, "wget": {}, "ssh": {}, "exec": {}, "eval": {}, "write": {}, "delete": {},
}

// ShellTokenClass classifies the first token of a shell command for shell_safe policy.
type ShellTokenClass int

const (
	ShellTokenUnknown ShellTokenClass = iota
	ShellTokenReadOnly
	ShellTokenGate
)

// ShellCommandRequiresApproval reports whether a command string must be gated under shell_safe.
//
// This is a first-token heuristic with metacharacter fail-closed checks — not a sandbox.
// Commands containing shell composition syntax (;|&$`, newlines, $(…)) always require approval.
func ShellCommandRequiresApproval(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return true
	}
	if containsShellMetacharacters(command) {
		return true
	}
	switch ClassifyShellToken(FirstShellToken(command)) {
	case ShellTokenReadOnly:
		return false
	default:
		return true
	}
}

func containsShellMetacharacters(command string) bool {
	if strings.Contains(command, "$(") {
		return true
	}
	for _, r := range command {
		switch r {
		case ';', '|', '&', '\n', '\r', '`', '$':
			return true
		}
	}
	return false
}

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

// IsShellCommandOperation reports whether operation is a shell command carrier for shell_safe.
func IsShellCommandOperation(operation string) bool {
	op := strings.ToLower(strings.TrimSpace(operation))
	for _, candidate := range ShellCommandOperations {
		if op == candidate {
			return true
		}
	}
	return false
}
