-- Tenant/thread/actor attribution for runs and trace events (issue #111).

ALTER TABLE runs ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant-1';
ALTER TABLE runs ADD COLUMN thread_id TEXT NOT NULL DEFAULT 'thread-1';
ALTER TABLE runs ADD COLUMN actor_id TEXT NOT NULL DEFAULT 'user-1';
ALTER TABLE runs ADD COLUMN parent_run_id TEXT;
ALTER TABLE runs ADD COLUMN request_id TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN idempotency_key TEXT;
ALTER TABLE runs ADD COLUMN source TEXT NOT NULL DEFAULT 'cli';

ALTER TABLE trace_events ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant-1';
ALTER TABLE trace_events ADD COLUMN thread_id TEXT NOT NULL DEFAULT 'thread-1';
ALTER TABLE trace_events ADD COLUMN actor_id TEXT NOT NULL DEFAULT 'user-1';

UPDATE trace_events
SET
  tenant_id = (SELECT tenant_id FROM runs WHERE runs.run_id = trace_events.run_id),
  thread_id = (SELECT thread_id FROM runs WHERE runs.run_id = trace_events.run_id),
  actor_id = (SELECT actor_id FROM runs WHERE runs.run_id = trace_events.run_id);

UPDATE runs SET request_id = run_id WHERE request_id = '';

CREATE INDEX IF NOT EXISTS idx_runs_tenant_thread ON runs (tenant_id, thread_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_runs_actor ON runs (actor_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_trace_events_tenant_thread ON trace_events (tenant_id, thread_id);
