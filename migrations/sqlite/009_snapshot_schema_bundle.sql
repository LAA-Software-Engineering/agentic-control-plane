-- Capture referenced JSON Schemas in the deployment snapshot (issue #207 follow-up). A pinned
-- resume validates workflow input / agent output against the schema bytes captured at run start
-- (kind = schema_bundle in deployment_artifacts), rather than re-reading a possibly-changed file
-- from disk. Empty for snapshots that captured no schemas (backward compatible).

ALTER TABLE deployment_snapshots ADD COLUMN schema_bundle_digest TEXT NOT NULL DEFAULT '';
