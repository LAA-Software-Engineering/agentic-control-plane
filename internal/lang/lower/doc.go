// Package lower turns the #196 typed .agent AST into the existing resource
// model — the resource projection of ADR 002 §5.
//
// # What this projection is (and is not)
//
// ADR 002 §5 splits a checked .agent program into two SIBLING projections, not a
// pipeline:
//
//   - the RESOURCE projection (this package): Agent/Tool/Policy resources and a
//     Workflow's identity + dependency graph. It is what plan diffs, what apply
//     writes, and what policy/effect analysis runs against. It never gains an
//     expression language.
//   - the EXECUTION lowering (#199): InvokeTool/InvokeAgent/Fork/Join and the
//     control-flow forms Branch/Loop/Return. It is never hand-authored and has no
//     YAML surface.
//
// The naive reading "AST -> resource IR -> execution IR" is explicitly wrong
// (ADR 002 §5): control flow cannot be recovered from the resource projection
// once it exists, so the execution lowering must read the same checked program
// independently. This package is therefore written so #199 is ADDITIVE — nothing
// downstream is expected to reconstruct control flow from [Result]. Do not make
// the execution lowering consume a [Result]; make it a second reader of the AST.
//
// Until the checker (#198) lands the "checked program" is the raw AST, so
// [LowerFile] takes an [ast]/[lang].File directly. Reference resolution, typing,
// and effect checking are #198; effect-clause and type storage are deferred to
// them (recorded in the [SourceMap], not forced into resource-model fields — see
// the package plan docs/plans/197-lowering.md).
//
// # Identity vs location (ADR 003)
//
// Generated step identity derives from STRUCTURAL position in the program
// (enclosing workflow, binding name, AST child path), never from source
// coordinates. spec.Pos is diagnostic metadata only. The invariant is
// identity != location: an unrelated edit above a binding, or reformatting, must
// produce a byte-identical resource graph. See stepID and the stability test.
package lower
