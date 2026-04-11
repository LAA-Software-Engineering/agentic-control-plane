package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	_ "modernc.org/sqlite" // register "sqlite" driver
)

// Store persists deployment state (§14.1) and runtime/trace state (§14.2) in SQLite.
type Store struct {
	db *sql.DB
}

// Open opens or creates a database at dsn and runs migrations.
// dsn is passed to database/sql (e.g. absolute path to a .db file); see modernc.org/sqlite docs.
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	// SQLite disables FK checks by default; enforce per connection. With MaxOpenConns(1) this
	// covers the pooled connection used for all statements on this Store.
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite foreign_keys: %w", err)
	}
	if err := Migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// UpsertAppliedResource inserts or replaces a row for (kind, name, env).
func (s *Store) UpsertAppliedResource(ctx context.Context, r state.AppliedResource) error {
	return upsertAppliedResource(ctx, s.db, r)
}

// GetAppliedResource returns the row for env and ResourceID, or sql.ErrNoRows.
func (s *Store) GetAppliedResource(ctx context.Context, env string, id spec.ResourceID) (*state.AppliedResource, error) {
	return getAppliedResource(ctx, s.db, env, id)
}

// ListAppliedResourcesByEnv lists all applied resources for the given environment.
func (s *Store) ListAppliedResourcesByEnv(ctx context.Context, env string) ([]state.AppliedResource, error) {
	return listAppliedResourcesByEnv(ctx, s.db, env)
}

// DeleteAppliedResource removes one applied_resources row. It is idempotent: deleting a
// non-existent row returns nil.
func (s *Store) DeleteAppliedResource(ctx context.Context, env string, id spec.ResourceID) error {
	return deleteAppliedResource(ctx, s.db, env, id)
}

// UpsertAppliedProject inserts or replaces a row for (project_name, env).
func (s *Store) UpsertAppliedProject(ctx context.Context, p state.AppliedProject) error {
	return upsertAppliedProject(ctx, s.db, p)
}

// GetAppliedProject returns the row for project name and env, or sql.ErrNoRows.
func (s *Store) GetAppliedProject(ctx context.Context, env, projectName string) (*state.AppliedProject, error) {
	return getAppliedProject(ctx, s.db, env, projectName)
}
