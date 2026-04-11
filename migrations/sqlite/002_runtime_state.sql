-- Runtime / trace tables (design doc §14.2, issue #10).
-- Referential integrity: FOREIGN KEY to runs; requires PRAGMA foreign_keys=ON per connection.

CREATE TABLE IF NOT EXISTS runs (
  run_id TEXT NOT NULL PRIMARY KEY,
  workflow_name TEXT NOT NULL,
  env TEXT NOT NULL,
  status TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  input_json TEXT NOT NULL DEFAULT '{}',
  output_json TEXT,
  error_text TEXT,
  total_cost_usd REAL NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_runs_workflow_started ON runs (workflow_name, started_at);

CREATE TABLE IF NOT EXISTS run_steps (
  run_id TEXT NOT NULL,
  step_id TEXT NOT NULL,
  status TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  input_json TEXT,
  output_json TEXT,
  error_text TEXT,
  cost_usd REAL NOT NULL DEFAULT 0,
  PRIMARY KEY (run_id, step_id),
  FOREIGN KEY (run_id) REFERENCES runs (run_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS trace_events (
  run_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  timestamp TEXT NOT NULL,
  type TEXT NOT NULL,
  step_id TEXT,
  data_json TEXT NOT NULL DEFAULT '{}',
  PRIMARY KEY (run_id, seq),
  FOREIGN KEY (run_id) REFERENCES runs (run_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_trace_events_run ON trace_events (run_id);
