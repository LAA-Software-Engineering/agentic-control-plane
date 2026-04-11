// Package apply applies plans to control-plane and runtime state.
//
// [Applier.ApplyPlan] writes applied_resources and applied_projects using [plan.Plan] operations.
// SQLite uses a single transaction via [state.TransactionalDeployment] (issue #15).
package apply
