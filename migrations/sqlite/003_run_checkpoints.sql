-- Run checkpoints for pause/resume (issue #105).
-- Referential integrity: FOREIGN KEY to runs; requires PRAGMA foreign_keys=ON per connection.

CREATE TABLE IF NOT EXISTS run_checkpoints (
  run_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  step_index INTEGER NOT NULL,
  step_id TEXT NOT NULL,
  context_json TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (run_id, seq),
  FOREIGN KEY (run_id) REFERENCES runs (run_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_run_checkpoints_run ON run_checkpoints (run_id, seq DESC);
