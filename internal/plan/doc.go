// Package plan computes desired vs current state diffs and risk summaries.
//
// Deployment comparison uses canonical JSON from encoding/json and spec_hash = SHA-256(hex)
// of those bytes (design doc §14.1, issue #12). [RiskSummary] is filled from Policy, Agent, and
// Tool diffs (issues #13, #165); tool mutating risk uses [ActionSuggestsWriteSideEffects].
// Structured [RiskItem] values carry category, severity, target, and an optional [WitnessHop]
// path (static or autonomous edges) so later effect-delta work (issue #191) can attach
// Workflow→step→Agent→tool.operation witnesses without a parallel rendering path.
// Table output groups items by severity; JSON/YAML share [ExportRisk] / [AttachRiskExport]
// ([RiskSummary.Messages] remains the reason text of Items for existing string consumers).
package plan
