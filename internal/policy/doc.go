// Package policy evaluates permissions, budgets, and approval rules for runs, steps, and tool calls
// (design doc section 12.2 H MVP).
//
// Use [NewEngine] with a loaded [spec.ProjectGraph], then [Engine.Evaluator] or [Engine.EvaluatorForSpec].
// Run context should carry elapsed wall clock, accumulated cost, and repeated --approve action strings
// matching policy approvals.requiredFor entries.
package policy
