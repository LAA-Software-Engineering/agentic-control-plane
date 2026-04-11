package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// querier is implemented by *sql.DB and *sql.Tx for deployment table access.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func upsertAppliedResource(ctx context.Context, q querier, r state.AppliedResource) error {
	at := r.AppliedAt.UTC().Format(time.RFC3339Nano)
	_, err := q.ExecContext(ctx, `
INSERT INTO applied_resources (kind, name, env, spec_hash, normalized_spec_json, applied_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(kind, name, env) DO UPDATE SET
  spec_hash = excluded.spec_hash,
  normalized_spec_json = excluded.normalized_spec_json,
  applied_at = excluded.applied_at
`, r.Kind, r.Name, r.Env, r.SpecHash, r.NormalizedSpecJSON, at)
	return err
}

func getAppliedResource(ctx context.Context, q querier, env string, id spec.ResourceID) (*state.AppliedResource, error) {
	row := q.QueryRowContext(ctx, `
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

func listAppliedResourcesByEnv(ctx context.Context, q querier, env string) ([]state.AppliedResource, error) {
	rows, err := q.QueryContext(ctx, `
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

func deleteAppliedResource(ctx context.Context, q querier, env string, id spec.ResourceID) error {
	_, err := q.ExecContext(ctx, `
DELETE FROM applied_resources
WHERE env = ? AND kind = ? AND name = ?
`, env, id.Kind, id.Name)
	return err
}

func upsertAppliedProject(ctx context.Context, q querier, p state.AppliedProject) error {
	at := p.AppliedAt.UTC().Format(time.RFC3339Nano)
	_, err := q.ExecContext(ctx, `
INSERT INTO applied_projects (project_name, env, version, applied_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(project_name, env) DO UPDATE SET
  version = excluded.version,
  applied_at = excluded.applied_at
`, p.ProjectName, p.Env, p.Version, at)
	return err
}

func getAppliedProject(ctx context.Context, q querier, env, projectName string) (*state.AppliedProject, error) {
	row := q.QueryRowContext(ctx, `
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
