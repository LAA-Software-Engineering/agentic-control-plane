// Package sqlite implements deployment and runtime/trace state in SQLite (design doc §§14.1–14.2).
// Use [Open] with a file DSN; [Migrate] runs versioned SQL from /migrations/sqlite.
//
// Consistency: [Open] runs PRAGMA foreign_keys=ON after the first connection is established
// (with MaxOpenConns(1), that matches the single pooled connection). SQLite then enforces FOREIGN
// KEY from run_steps and trace_events to runs (including ON DELETE CASCADE). Callers may still
// validate run_id in the application for clearer errors; the pragma is the DB-level guarantee.
package sqlite
