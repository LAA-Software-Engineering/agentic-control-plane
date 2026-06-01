// Package spec defines resource envelopes, MVP kind structs (§6–§7), YAML loading,
// project-level defaults ([NormalizeProjectGraph]), reference resolution ([ResolveReferences]),
// and graph validation ([ValidateProjectGraph], §9.1–§9.5 MVP subset).
// Environment-specific overrides (§7.6) are applied by [github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime/local.ApplyEnvironment]
// after [NormalizeProjectGraph] in CLI and local-runtime flows.
//
// Tool resources may declare spec.safety (trusted, sideEffects, requiresApproval) for
// fail-closed policy derivation when explicit Policy rules do not apply (issue #103).
package spec
