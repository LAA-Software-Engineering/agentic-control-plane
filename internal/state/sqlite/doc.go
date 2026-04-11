// Package sqlite implements deployment state storage in SQLite (design doc §14.1).
// Use [Open] with a file DSN; [Migrate] runs versioned SQL from /migrations/sqlite.
package sqlite
