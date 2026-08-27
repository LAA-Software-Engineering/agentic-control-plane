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

func latestSnapshotDigestForEnv(ctx context.Context, q querier, env string) (string, error) {
	row := q.QueryRowContext(ctx, `
SELECT digest
FROM deployment_snapshots
WHERE environment = ?
ORDER BY created_at DESC, digest DESC
LIMIT 1
`, env)
	var digest string
	if err := row.Scan(&digest); err != nil {
		return "", err
	}
	return digest, nil
}

// pruneUnreferencedArtifacts removes snapshots no run references and artifacts no surviving
// snapshot references. Reference-guarded so trace pruning (which deletes runs) cannot orphan an
// artifact a surviving run still needs to resume.
func pruneUnreferencedArtifacts(ctx context.Context, q querier) (int64, error) {
	snapRes, err := q.ExecContext(ctx, `
DELETE FROM deployment_snapshots
WHERE digest NOT IN (
  SELECT deployment_snapshot_digest FROM runs WHERE deployment_snapshot_digest <> ''
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
