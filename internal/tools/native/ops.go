package native

import "github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"

// OperationKnown reports whether operation is implemented by [Registry.Dispatch].
func OperationKnown(operation string) bool {
	_, ok := nativeOperationArgs[operation]
	return ok || spec.IsShellCommandOperation(operation)
}

// TopLevelArgsForOperation returns known top-level input keys for operation.
// The second value is false when the operation is unknown or accepts arbitrary keys (echo).
func TopLevelArgsForOperation(operation string) ([]string, bool) {
	if spec.IsShellCommandOperation(operation) {
		return []string{"command", "cmd", "script"}, true
	}
	args, ok := nativeOperationArgs[operation]
	if !ok {
		return nil, false
	}
	if args == nil {
		return nil, true
	}
	return append([]string(nil), args...), true
}

// nativeOperationArgs maps operation names to known top-level args.
// A nil slice means arbitrary keys are accepted (echo).
var nativeOperationArgs = map[string][]string{
	"echo":               nil,
	"identity":           {"value"},
	"pull_request.fetch": {"pr"},
	"pull_request.post_comment": {
		"body", "owner", "repo", "number", "comment_id", "comment_strategy",
	},
	"pull_request.get":  {"owner", "repo", "number"},
	"pull_request.diff": {"owner", "repo", "number"},
	"check_runs.list":   {"owner", "repo", "ref"},
}
