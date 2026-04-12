// Package cli defines agentctl commands, global flags (design doc section 11.1), and exit-code
// mapping (section 11.2). Output formatting lives in [render].
//
// The validate command (section 10.2) loads the project, applies defaults and optional environment
// overlays, then runs [spec.ValidateProjectGraph].
//
// The plan command compares that prepared graph to the SQLite deployment store (default
// .agentic/state.db, or project.spec.state.dsn / --state) and prints a diff plus risk delta
// via [plan.ComputePlan] and [plan.FormatPlan].
//
// The apply command runs the same preparation and planning, then prompts on a TTY (unless
// --auto-approve or AGENTCTL_AUTO_APPROVE) and persists via [apply.Applier.ApplyPlan].
package cli
