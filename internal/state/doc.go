// Package state stores deployment state and runtime metadata (design doc §5.2, §14).
//
// [DeploymentStore] and [RuntimeStore] are the boundaries plan, apply, and runtime code should use
// so callers do not depend on a specific SQL backend. MVP implements them in internal/state/sqlite.
//
// Thread-safety: interfaces assume a single-process CLI unless a concrete backend documents
// stronger concurrency guarantees.
package state
