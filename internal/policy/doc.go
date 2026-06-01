// Package policy evaluates permissions, budgets, and approval rules for runs, steps, and tool calls
// (design doc section 12.2 H MVP).
//
// Use [NewEngine] with a loaded [spec.ProjectGraph], then [Engine.Evaluator] or [Engine.EvaluatorForSpec].
// Run context should carry elapsed wall clock, accumulated cost, and repeated --approve action strings
// matching policy approvals.requiredFor entries.
//
// When no explicit approvals.requiredFor rule matches a tool call, [Derive] consults
// [spec.ResolveToolSafety] metadata (fail-closed defaults; issue #103).
//
// # Tool-level safety vs per-action policy
//
// [spec.ToolSafety] applies to the whole Tool resource, not individual operations. Setting
// trusted: true allows unattended calls for every tool.<name>.<operation> unless an exact
// approvals.requiredFor entry blocks that full uses string. Gate writes with requiredFor, not
// by assuming trusted means "read-only only."
//
// # Plan vs runtime
//
// [EffectiveToolDecision] uses prefix matching on tool.<name>. for plan risk (conservative:
// any listed action under the tool flags the whole Tool). Runtime [approvalRequired] matches
// the full uses string exactly.
package policy
