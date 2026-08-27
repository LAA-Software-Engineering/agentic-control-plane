-- Immutable content-addressed deployment artifacts and snapshots (design doc §14, issue #207).
-- A run pins one deployment_snapshot; resume hydrates configuration and authority from it rather
-- than re-resolving current config, so an apply landing mid-run cannot change an in-flight run's
-- authority. Artifacts are deduped by content digest and never mutated once written.

CREATE TABLE IF NOT EXISTS deployment_artifacts (
  digest TEXT NOT NULL PRIMARY KEY,
  kind TEXT NOT NULL,
  format_version TEXT NOT NULL,
  payload BLOB NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS deployment_snapshots (
  digest TEXT NOT NULL PRIMARY KEY,
  format_version TEXT NOT NULL,
  compiler_version TEXT NOT NULL,
  environment TEXT NOT NULL,
  graph_digest TEXT NOT NULL,
  execution_ir_digest TEXT NOT NULL DEFAULT '',
  capability_manifest_digest TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_deployment_snapshots_env_created
  ON deployment_snapshots (environment, created_at);

-- Current deployed snapshot per environment. Updated on EVERY apply (including a re-apply of an
-- earlier digest — a rollback A -> B -> A), so "superseded" means "differs from what is deployed
-- now", not "differs from the oldest-max created_at row". Content-addressed snapshot rows are
-- immutable and cannot double as a recency index, so this pointer is a separate, mutable row.
CREATE TABLE IF NOT EXISTS deployment_env_current (
  environment TEXT NOT NULL PRIMARY KEY,
  snapshot_digest TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

-- One column on runs, forever: the pinned deployment snapshot root.
ALTER TABLE runs ADD COLUMN deployment_snapshot_digest TEXT NOT NULL DEFAULT '';
