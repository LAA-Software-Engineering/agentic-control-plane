package policy

import (
	"fmt"

	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/tools"
)

// checkOperationInManifest enforces the closed-world capability manifest (issue #204, ADR 002).
//
// The deployed capability manifest is the sound upper bound on the callable universe: no operation
// may become agent-callable unless it was present in that manifest. A runtime tools/list may
// advertise anything; an operation absent from the tool's declared manifest is denied here on the
// policy path, so it carries the existing denial exit-code semantics (exit 5) and emits a trace
// event via [DeniedError.TraceData]. A silently dropped tool call is worse than a loud failure.
//
// Enforcement is intrinsic to the manifest, not gated on an approval or forbidUnknownTools flag: a
// permissive policy relaxes approvals, never the closed world. It is a no-op for a tool that
// declares no operations (an open callable set), so existing MCP/HTTP examples are unaffected.
//
// NOTE (issue #207): graph is the resolved graph for this run. The run-pinned deployed manifest —
// enforcing the manifest a suspended run started with, not whatever is deployed at resume time —
// is reached through the run's deployment snapshot and is deferred to #207. Until then this
// enforces against the run's resolved graph, whose manifest is the declared spec.operations and is
// never populated from live discovery.
func checkOperationInManifest(graph *spec.ProjectGraph, uses string) error {
	toolName, operation, err := tools.ParseUses(uses)
	if err != nil {
		// A malformed uses string is denied downstream (checkSafetyDerived) with the existing
		// invalid_uses semantics; don't change that path or the permissive-policy short-circuit.
		return nil
	}
	m := tools.ManifestFor(graph, toolName)
	if m.Allows(operation) {
		return nil
	}
	return denied(
		ReasonOperationNotInManifest,
		fmt.Sprintf("policy: operation %q is not in the deployed capability manifest for tool %q", operation, toolName),
		uses,
		map[string]any{"tool": toolName, "operation": operation},
	)
}
