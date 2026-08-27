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
//   - the EXECUTION lowering ([LowerExec], #199): InvokeTool/InvokeAgent/
//     InvokeWorkflow/Fork and the control-flow forms Branch/Loop/Return, emitted
//     into internal/execir. It is never hand-authored and has no YAML surface.
//
// The naive reading "AST -> resource IR -> execution IR" is explicitly wrong
// (ADR 002 §5): control flow cannot be recovered from the resource projection,
// so the execution lowering reads the same AST independently. [LowerExec] is
// accordingly a SECOND reader of the AST, not a consumer of [Result]; the two
// lowerings share only the callee-classification rule (dotted callee = tool;
// single identifier = workflow when named, else agent). Their one coupling is by
// design: [LowerFile] additionally FLATTENS every conditional arm and loop body
// into resource steps (lowerControlStmts) so the effect bound computed over the
// resource projection is the union over all branches — a conditional cannot
// smuggle an unpermitted effect past the effects clause (ADR 002 §5).
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
