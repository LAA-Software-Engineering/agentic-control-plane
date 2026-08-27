// Package check implements the ADR 002 §5 "checked program": the pass that
// sits between the #196 typed AST and the two sibling projections (the #197
// resource projection and the #199 execution lowering, internal/execir). Check
// lowers both projections and exposes the execution IR on Program.Executables.
//
//	.agent  ->  typed AST  ->  checked program  ->  { resource projection, execution lowering }
//	                           (resolved refs · types · effect bound)
//
// Check resolves cross-declaration references, resolves TypeRef names against
// loaded JSON Schema documents, and computes the actual reachable effect bound
// for every workflow so the workflow's `effects { }` clause becomes a checked
// declaration rather than documentation (issue #198).
//
// Deliberate non-goal: this package does NOT reimplement effect-graph
// traversal. It lowers every file in the compilation unit through the
// existing internal/lang/lower (issue #197) into one merged
// *spec.ProjectGraph and calls the existing, unmodified internal/effects.Compute
// (issue #189) — the same function the YAML ingress path already uses. Two
// independent effect walkers could only be tested into agreement and would
// drift the next time either changed; this package makes "the frontend and
// YAML paths produce identical bounds" true by construction — which in turn
// requires lowering the WHOLE compilation unit, not just the file under
// check: a callee classified but never itself lowered would leave
// effects.Compute walking a resource that is not in the graph.
//
// This package also introduces one new convention with no prior ADR or
// grammar text: a TypeRef name resolves to a schema file at
// <SchemaDir>/schemas/<Name>.json. See doc.go comments in types.go for the
// gradual-typing rules around it (a missing file is untyped; a file that
// exists but fails to compile is reported, not swallowed).
package check
