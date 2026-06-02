-- Resume pinning metadata (PR #127 review): workflow spec hash and environment overlay name.

ALTER TABLE runs ADD COLUMN workflow_spec_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN environment_name TEXT NOT NULL DEFAULT '';
