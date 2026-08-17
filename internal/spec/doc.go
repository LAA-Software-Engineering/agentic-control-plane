// Package spec defines resource envelopes, MVP kind structs (§6–§7), YAML loading,
// project-level defaults ([NormalizeProjectGraph]), reference resolution ([ResolveReferences]),
// and graph validation ([ValidateProjectGraph], §9.1–§9.5 MVP subset).
// Environment-specific overrides (§7.6) are applied by [ApplyEnvironment].
// after [NormalizeProjectGraph] in CLI and local-runtime flows.
//
// Tool resources may declare spec.safety (trusted, sideEffects, requiresApproval) for
// fail-closed policy derivation when explicit Policy rules do not apply (issue #103).
// spec.operations names per-operation effects (issue #188); undeclared tools are fail-closed
// in [ResolveToolEffects] and do not change runtime CheckToolCall.
//
// Built-in policy presets (strict, permissive, shell_safe) are defined in this package and
// expanded during [NormalizeProjectGraph] via [ExpandPresetsInGraph] (issue #104).
package spec
