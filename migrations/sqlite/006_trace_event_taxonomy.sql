-- Trace event taxonomy (issue #115): actor_type column + migrate legacy type strings.
-- Legacy type → canonical mappings must match internal/trace/legacy.go (LegacyEventTypeMappings).

ALTER TABLE trace_events ADD COLUMN actor_type TEXT NOT NULL DEFAULT 'system';

UPDATE trace_events SET type = 'run_started', actor_type = 'agent' WHERE type = 'run.started';
UPDATE trace_events SET type = 'run_finished', actor_type = 'agent' WHERE type = 'run.finished';
UPDATE trace_events SET type = 'run_error', actor_type = 'system' WHERE type IN ('run.interrupted', 'step.failed');
UPDATE trace_events SET type = 'run_started', actor_type = 'agent' WHERE type = 'run.resumed';
UPDATE trace_events SET type = 'tool_selection', actor_type = 'agent' WHERE type IN ('step.started', 'tool.called');
UPDATE trace_events SET type = 'tool_execution', actor_type = 'agent' WHERE type IN ('step.finished', 'tool.completed');
UPDATE trace_events SET type = 'llm_completion', actor_type = 'agent' WHERE type IN ('model.called', 'model.completed');
UPDATE trace_events SET type = 'system_error', actor_type = 'system' WHERE type = 'policy.denied';
UPDATE trace_events SET type = 'hitl_request_created', actor_type = 'system' WHERE type = 'approval.requested';
UPDATE trace_events SET type = 'hitl_decision_submitted', actor_type = 'user' WHERE type = 'approval.resolved';

UPDATE trace_events SET actor_type = 'agent'
WHERE actor_type = 'system' AND type IN (
  'run_started', 'run_finished', 'tool_selection', 'tool_execution', 'llm_completion'
);

CREATE INDEX IF NOT EXISTS idx_trace_events_type ON trace_events (type);
