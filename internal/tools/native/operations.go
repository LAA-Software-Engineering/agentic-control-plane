package native

import (
	"sort"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// DispatchOperations lists operation names handled by [Registry.Dispatch] (excluding shell ops).
// Derived from dispatchHandlers; keep operationCatalog in sync (see TestRegistryDispatchMatchesCatalog).
var DispatchOperations = dispatchOperationNames()

func dispatchOperationNames() []string {
	names := make([]string, 0, len(dispatchHandlers))
	for name := range dispatchHandlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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
