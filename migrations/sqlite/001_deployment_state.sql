-- Deployment state tables (design doc §14.1, issue #9).
-- Applied as version 1 by internal/state/sqlite migrations.

CREATE TABLE IF NOT EXISTS applied_resources (
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  env TEXT NOT NULL,
  spec_hash TEXT NOT NULL,
  normalized_spec_json TEXT NOT NULL,
  applied_at TEXT NOT NULL,
  PRIMARY KEY (kind, name, env)
);

CREATE TABLE IF NOT EXISTS applied_projects (
  project_name TEXT NOT NULL,
  env TEXT NOT NULL,
  version TEXT NOT NULL,
  applied_at TEXT NOT NULL,
  PRIMARY KEY (project_name, env)
);
