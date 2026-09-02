package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
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
	if err := applyPerformancePragmas(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := Migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// applyPerformancePragmas tunes the write path, which is dominated by the fsync each transaction
// issues while building the schema and appending run/trace rows. The default DELETE journal fsyncs
// on every commit; that cost is what makes the file-backed store slow on runners with slow disk
// I/O (notably Windows CI, further amplified by antivirus scanning of every temp .db and by -race).
//
// Production keeps full durability: WAL commits are still fsynced, and synchronous=NORMAL only
// relaxes the *checkpoint* fsync — a WAL+NORMAL database survives an application crash and loses at
// most the last transaction on OS/power loss. busy_timeout makes concurrent opens of the same file
// wait rather than fail with SQLITE_BUSY.
//
// Under `go test` the databases are throwaway TempDir files, so we trade durability for speed:
// synchronous=OFF removes every fsync and journal_mode=MEMORY keeps the rollback journal in RAM,
// which also avoids creating the -wal/-shm/-journal sidecar files that antivirus would otherwise
// scan. Correctness within a run is unaffected: MaxOpenConns(1) means a single connection, and a
// crash mid-test only discards a database we were about to delete anyway.
func applyPerformancePragmas(ctx context.Context, db *sql.DB) error {
	pragmas := []string{
		`PRAGMA busy_timeout=5000`,
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
	}
	if testing.Testing() {
		pragmas = []string{
			`PRAGMA busy_timeout=5000`,
			`PRAGMA journal_mode=MEMORY`,
			`PRAGMA synchronous=OFF`,
		}
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			return fmt.Errorf("sqlite pragma %q: %w", p, err)
		}
	}
	return nil
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

// PutArtifact stores an immutable content-addressed artifact, deduped by digest (issue #207).
func (s *Store) PutArtifact(ctx context.Context, a state.DeploymentArtifact) error {
	return putArtifact(ctx, s.db, a)
}

// GetArtifact returns the artifact for digest, or sql.ErrNoRows.
func (s *Store) GetArtifact(ctx context.Context, digest string) (*state.DeploymentArtifact, error) {
	return getArtifact(ctx, s.db, digest)
}

// PutSnapshot stores a deployment snapshot row, deduped by digest (issue #207).
func (s *Store) PutSnapshot(ctx context.Context, snap state.DeploymentSnapshot) error {
	return putSnapshot(ctx, s.db, snap)
}

// GetSnapshot returns the snapshot for digest, or sql.ErrNoRows.
func (s *Store) GetSnapshot(ctx context.Context, digest string) (*state.DeploymentSnapshot, error) {
	return getSnapshot(ctx, s.db, digest)
}

// SetCurrentSnapshot points env at the snapshot deployed now (issue #207).
func (s *Store) SetCurrentSnapshot(ctx context.Context, env, digest string) error {
	return setCurrentSnapshot(ctx, s.db, env, digest, time.Now())
}

// CurrentSnapshotDigestForEnv returns the snapshot digest currently deployed for env, or sql.ErrNoRows.
func (s *Store) CurrentSnapshotDigestForEnv(ctx context.Context, env string) (string, error) {
	return currentSnapshotDigestForEnv(ctx, s.db, env)
}

// PruneUnreferencedArtifacts removes snapshots/artifacts no run references (issue #207).
func (s *Store) PruneUnreferencedArtifacts(ctx context.Context) (int64, error) {
	return pruneUnreferencedArtifacts(ctx, s.db)
}
