-- Tamper-evident audit chain (issue #116): hash-linked trace_events per run.
-- Pre-existing rows keep NULL prev_hash/hash and are reported as "unchained" by audit verify.

ALTER TABLE trace_events ADD COLUMN prev_hash TEXT;
ALTER TABLE trace_events ADD COLUMN hash TEXT;
