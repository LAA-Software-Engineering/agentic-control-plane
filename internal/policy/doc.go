// Package policy evaluates permissions, budgets, and approval rules for runs, steps, and tool calls
// (design doc section 12.2 H MVP).
//
// Use [NewEngine] with a loaded [spec.ProjectGraph], then [Engine.Evaluator] or [Engine.EvaluatorForSpec].
// Run context should carry elapsed wall clock, accumulated cost, and repeated --approve action strings
// matching policy approvals.requiredFor entries.
//
// Built-in policy presets (issue #104): strict, permissive, and shell_safe. Select via
// Project.spec.defaults.policy, a Policy resource spec.preset, or by referencing a preset name
// as the workflow/agent policy. [spec.ExpandPresetsInGraph] materializes effective rules during normalize.
//
// shell_safe uses first-token heuristics plus metacharacter fail-closed checks — not a sandbox.
// Plan risk for shell_safe is tool-granular (conservative); runtime applies per-command classification.
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
//
// # Human-in-the-loop (issue #106)
//
// When a tool call would require approval, [BuildHitlGate] and [ResolveHitlReview] supply review
// configuration from Policy.spec.hitl. Operators resolve gates with approve, reject, edit, or switch;
// [ApplyHitlDecision] and [ValidateHitlEdit] enforce per-call edit and switch rules.
//
// # Static policy lint (issue #107)
//
// [Lint] runs at validate/plan time and returns structured findings (ungated sensitive tools, invalid
// HITL switch targets, unknown edit args, weakened presets, and similar). High-severity findings can
// fail validate when --strict is set.
package policy
