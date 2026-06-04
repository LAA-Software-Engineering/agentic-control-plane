package native

import (
	"sort"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// DispatchOperations lists operation names handled by [Registry.Dispatch] (excluding shell ops).
// Keep in sync with the switch in registry.go and operationCatalog below.
var DispatchOperations = []string{
	"check_runs.list",
	"echo",
	"identity",
	"pull_request.diff",
	"pull_request.fetch",
	"pull_request.get",
	"pull_request.post_comment",
}

// operationCatalog maps dispatch operation names to known top-level args (nil = arbitrary).
var operationCatalog = map[string][]string{
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

// OperationKnown reports whether operation is implemented by [Registry.Dispatch].
func OperationKnown(operation string) bool {
	for _, name := range DispatchOperations {
		if name == operation {
			return true
		}
	}
	return spec.IsShellCommandOperation(operation)
}

// DispatchOperationNames returns sorted dispatch operation names (excluding shell ops).
func DispatchOperationNames() []string {
	out := append([]string(nil), DispatchOperations...)
	sort.Strings(out)
	return out
}

// TopLevelArgsForOperation returns known top-level input keys for operation.
// The second value is false when the operation is unknown or accepts arbitrary keys (echo).
func TopLevelArgsForOperation(operation string) ([]string, bool) {
	if spec.IsShellCommandOperation(operation) {
		return []string{"command", "cmd", "script"}, true
	}
	args, ok := operationCatalog[operation]
	if !ok {
		return nil, false
	}
	if args == nil {
		return nil, true
	}
	return append([]string(nil), args...), true
}
