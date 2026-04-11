package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
	at := r.AppliedAt.UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO applied_resources (kind, name, env, spec_hash, normalized_spec_json, applied_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(kind, name, env) DO UPDATE SET
  spec_hash = excluded.spec_hash,
  normalized_spec_json = excluded.normalized_spec_json,
  applied_at = excluded.applied_at
`, r.Kind, r.Name, r.Env, r.SpecHash, r.NormalizedSpecJSON, at)
	return err
}

// GetAppliedResource returns the row for env and ResourceID, or sql.ErrNoRows.
func (s *Store) GetAppliedResource(ctx context.Context, env string, id spec.ResourceID) (*state.AppliedResource, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT kind, name, env, spec_hash, normalized_spec_json, applied_at
FROM applied_resources
WHERE env = ? AND kind = ? AND name = ?
`, env, id.Kind, id.Name)
	var r state.AppliedResource
	var at string
	if err := row.Scan(&r.Kind, &r.Name, &r.Env, &r.SpecHash, &r.NormalizedSpecJSON, &at); err != nil {
		return nil, err
	}
	t, err := parseSQLiteTime(at)
	if err != nil {
		return nil, fmt.Errorf("applied_at: %w", err)
	}
	r.AppliedAt = t
	return &r, nil
}

// ListAppliedResourcesByEnv lists all applied resources for the given environment.
func (s *Store) ListAppliedResourcesByEnv(ctx context.Context, env string) ([]state.AppliedResource, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT kind, name, env, spec_hash, normalized_spec_json, applied_at
FROM applied_resources
WHERE env = ?
ORDER BY kind, name
`, env)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []state.AppliedResource
	for rows.Next() {
		var r state.AppliedResource
		var at string
		if err := rows.Scan(&r.Kind, &r.Name, &r.Env, &r.SpecHash, &r.NormalizedSpecJSON, &at); err != nil {
			return nil, err
		}
		t, err := parseSQLiteTime(at)
		if err != nil {
			return nil, err
		}
		r.AppliedAt = t
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertAppliedProject inserts or replaces a row for (project_name, env).
func (s *Store) UpsertAppliedProject(ctx context.Context, p state.AppliedProject) error {
	at := p.AppliedAt.UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO applied_projects (project_name, env, version, applied_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(project_name, env) DO UPDATE SET
  version = excluded.version,
  applied_at = excluded.applied_at
`, p.ProjectName, p.Env, p.Version, at)
	return err
}

// GetAppliedProject returns the row for project name and env, or sql.ErrNoRows.
func (s *Store) GetAppliedProject(ctx context.Context, env, projectName string) (*state.AppliedProject, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT project_name, env, version, applied_at
FROM applied_projects
WHERE env = ? AND project_name = ?
`, env, projectName)
	var p state.AppliedProject
	var at string
	if err := row.Scan(&p.ProjectName, &p.Env, &p.Version, &at); err != nil {
		return nil, err
	}
	t, err := parseSQLiteTime(at)
	if err != nil {
		return nil, err
	}
	p.AppliedAt = t
	return &p, nil
}

func parseSQLiteTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
