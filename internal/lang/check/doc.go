// Package check implements the ADR 002 §5 "checked program": the pass that
// sits between the #196 typed AST and the two sibling projections (the #197
// resource projection and a future #199 execution lowering).
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
// traversal. It lowers the AST through the existing internal/lang/lower
// (issue #197) into a *spec.ProjectGraph and calls the existing, unmodified
// internal/effects.Compute (issue #189) — the same function the YAML ingress
// path already uses. Two independent effect walkers could only be tested into
// agreement and would drift the next time either changed; this package makes
// "the frontend and YAML paths produce identical bounds" true by construction.
// See docs/plans/198-type-effect-checking.md for the full design rationale,
// including the TypeRef-to-schema-file naming convention this package
// introduces (design decision 4 — flagged there as needing review).
package check
