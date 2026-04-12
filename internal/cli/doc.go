// Package cli defines agentctl commands, global flags (design doc section 11.1), and exit-code
// mapping (section 11.2). Output formatting lives in [render].
//
// The init command scaffolds a minimal project from embedded templates (section 10.2).
//
// The validate command (section 10.2) loads the project, applies defaults and optional environment
// overlays, then runs [spec.ValidateProjectGraph].
//
// The plan command compares that prepared graph to the SQLite deployment store (default
// .agentic/state.db, or project.spec.state.dsn / --state) and prints a diff plus risk delta
// via [plan.ComputePlan] and [plan.FormatPlan].
//
// The diff command uses the same comparison but prints a detailed per-resource view (field-level
// updates, full JSON for creates/deletes) and supports an optional Kind/name argument (§10.2).
//
// The apply command runs the same preparation and planning, then prompts on a TTY (unless
// --auto-approve or AGENTCTL_AUTO_APPROVE) and persists via [apply.Applier.ApplyPlan].
//
// The run command executes a workflow by name (workflow/<name>), validates input, writes trace
// rows to the same SQLite file as plan/apply, and maps policy denials to exit code 5.
//
// The logs command reads runs and trace_events from that SQLite file (ordered by seq per run)
// and supports listing recent runs, filtering by --run, or by --workflow.
package cli
