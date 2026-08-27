package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/LAA-Software-Engineering/terfyn/internal/state"
)

func putArtifact(ctx context.Context, q querier, a state.DeploymentArtifact) error {
	at := a.CreatedAt.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	// Immutable + content-addressed: dedupe on digest, never overwrite an existing payload.
	_, err := q.ExecContext(ctx, `
INSERT INTO deployment_artifacts (digest, kind, format_version, payload, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(digest) DO NOTHING
`, a.Digest, a.Kind, a.FormatVersion, a.Payload, at.Format(time.RFC3339Nano))
	return err
}

func getArtifact(ctx context.Context, q querier, digest string) (*state.DeploymentArtifact, error) {
	row := q.QueryRowContext(ctx, `
SELECT digest, kind, format_version, payload, created_at
FROM deployment_artifacts
WHERE digest = ?
`, digest)
	var a state.DeploymentArtifact
	var at string
	if err := row.Scan(&a.Digest, &a.Kind, &a.FormatVersion, &a.Payload, &at); err != nil {
		return nil, err
	}
	t, err := parseSQLiteTime(at)
	if err != nil {
		return nil, fmt.Errorf("created_at: %w", err)
	}
	a.CreatedAt = t
	return &a, nil
}

func putSnapshot(ctx context.Context, q querier, s state.DeploymentSnapshot) error {
	at := s.CreatedAt.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	_, err := q.ExecContext(ctx, `
INSERT INTO deployment_snapshots
  (digest, format_version, compiler_version, environment, graph_digest, execution_ir_digest, capability_manifest_digest, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(digest) DO NOTHING
`, s.Digest, s.FormatVersion, s.CompilerVersion, s.Environment, s.GraphDigest, s.ExecutionIRDigest, s.CapabilityManifestDigest, at.Format(time.RFC3339Nano))
	return err
}

func getSnapshot(ctx context.Context, q querier, digest string) (*state.DeploymentSnapshot, error) {
	row := q.QueryRowContext(ctx, `
SELECT digest, format_version, compiler_version, environment, graph_digest, execution_ir_digest, capability_manifest_digest, created_at
FROM deployment_snapshots
WHERE digest = ?
`, digest)
	var s state.DeploymentSnapshot
	var at string
	if err := row.Scan(&s.Digest, &s.FormatVersion, &s.CompilerVersion, &s.Environment, &s.GraphDigest, &s.ExecutionIRDigest, &s.CapabilityManifestDigest, &at); err != nil {
		return nil, err
	}
	t, err := parseSQLiteTime(at)
	if err != nil {
		return nil, fmt.Errorf("created_at: %w", err)
	}
	s.CreatedAt = t
	return &s, nil
}

// setCurrentSnapshot points env at digest, overwriting any prior pointer. Called on every apply,
// including a re-apply of an earlier digest, so the pointer tracks what is deployed now.
func setCurrentSnapshot(ctx context.Context, q querier, env, digest string, at time.Time) error {
	if at.IsZero() {
		at = time.Now()
	}
	_, err := q.ExecContext(ctx, `
INSERT INTO deployment_env_current (environment, snapshot_digest, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(environment) DO UPDATE SET
  snapshot_digest = excluded.snapshot_digest,
  updated_at = excluded.updated_at
`, env, digest, at.UTC().Format(time.RFC3339Nano))
	return err
}

// currentSnapshotDigestForEnv returns the snapshot digest currently deployed for env (the apply
// pointer), or sql.ErrNoRows when nothing has been applied to env.
func currentSnapshotDigestForEnv(ctx context.Context, q querier, env string) (string, error) {
	row := q.QueryRowContext(ctx, `SELECT snapshot_digest FROM deployment_env_current WHERE environment = ?`, env)
	var digest string
	if err := row.Scan(&digest); err != nil {
		return "", err
	}
	return digest, nil
}

// pruneUnreferencedArtifacts removes snapshots no run references AND that are not the current
// deployed snapshot for any environment, plus artifacts no surviving snapshot references.
// Reference-guarded so trace pruning (which deletes runs) can neither orphan an artifact a surviving
// run needs to resume, nor delete the current env identity that superseded detection depends on.
func pruneUnreferencedArtifacts(ctx context.Context, q querier) (int64, error) {
	snapRes, err := q.ExecContext(ctx, `
DELETE FROM deployment_snapshots
WHERE digest NOT IN (
  SELECT deployment_snapshot_digest FROM runs WHERE deployment_snapshot_digest <> ''
)
AND digest NOT IN (
  SELECT snapshot_digest FROM deployment_env_current
)
`)
	if err != nil {
		return 0, err
	}
	snaps, _ := snapRes.RowsAffected()

	artRes, err := q.ExecContext(ctx, `
DELETE FROM deployment_artifacts
WHERE digest NOT IN (
  SELECT graph_digest FROM deployment_snapshots WHERE graph_digest <> ''
  UNION SELECT execution_ir_digest FROM deployment_snapshots WHERE execution_ir_digest <> ''
  UNION SELECT capability_manifest_digest FROM deployment_snapshots WHERE capability_manifest_digest <> ''
)
`)
	if err != nil {
		return 0, err
	}
	arts, _ := artRes.RowsAffected()
	return snaps + arts, nil
}
