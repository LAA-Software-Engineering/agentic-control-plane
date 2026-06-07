# Tamper-evident trace audit chain (issue #116)

SQLite trace rows are the compliance evidence that an approved agent run is what actually executed. Plain rows can be edited silently; ACP hash-links each `trace_events` row into a **per-run chain** so post-hoc tampering (insert, delete, reorder, or mutate) is detectable with `agentctl audit verify`.

This is **internal consistency**, not cryptographic non-repudiation: there is no external timestamp authority or signing key yet. The chain proves the stored sequence matches the hash algorithm — useful for regulated deployments and forensic review.

## How it works

Each appended event stores:

| Column | Meaning |
| --- | --- |
| `prev_hash` | Hash of the previous **chained** event in this run, or a run-scoped genesis anchor for the first chained event |
| `hash` | `SHA-256(canonical_event_json ‖ prev_hash)` as lower-case hex |

**Canonical event JSON** covers exactly what is stored (after redaction, issue #110):

- `run_id`, `seq`, `timestamp` (RFC3339Nano UTC)
- `type`, `actor_type`, `step_id`
- `tenant_id`, `thread_id`, `actor_id` (issue #111 attribution)
- `data_json` (redacted payload string)

Hashing uses the **at-rest** `data_json` — verification never needs plaintext secrets.

Implementation: [`internal/audit`](../internal/audit).

## CLI

```bash
# Verify recent runs in the state DB (default: 50 newest, same as agentctl logs)
agentctl audit verify --project my-agent-system

# Verify one run (full chain for that run regardless of --limit)
agentctl audit verify --project my-agent-system --run <run-id>

# Scan more runs (clamped to 500)
agentctl audit verify --project my-agent-system --limit 200

# JSON output (-o json)
agentctl audit verify --project my-agent-system --run <run-id> -o json
```

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | All checked chains valid (unchained legacy rows are OK) |
| `1` | Chain break detected, or operational failure (e.g. cannot open SQLite) |
| `2` | Validation failure (unknown `--run` id) |

Table output example:

```text
State: /path/to/.agentic/state.db
run abc-123: OK (12 chained, 0 unchained)
run def-456: BROKEN at seq 3 (hash)
```

JSON output includes per-run `ok`, `chained`, `unchained`, and `brokenSeq` / `brokenAt` when broken.

## Backward compatibility

Migration `007_trace_audit_chain.sql` adds nullable `prev_hash` and `hash` columns. **Pre-existing rows are not backfilled** — they remain **unchained**:

- `audit verify` reports them in the `unchained` count
- They do **not** fail verification
- New events appended after unchained rows link from genesis (if no prior chained tip) or from the last chained event's hash

## JSON / inspector

`agentctl logs --run <id> -o json` and inspector trace payloads include optional `prevHash` and `hash` on each event when present.

## Operational notes

- Without `--run`, verification scans only the **most recent runs** (`--limit`, default **50**, max **500**) ordered by `started_at`. For a full-database audit, raise `--limit` or verify runs individually with `--run`.
- Run `audit verify` after backups/restores or manual DB edits in compliance workflows.
- Concurrent appends within one process are serialized by SQLite transactions; each run maintains a single chain tip.
- Future work (out of scope for #116): external signing, cross-run ledger, automatic verify in CI.

## Related docs

- [Run attribution](./ATTRIBUTION.md) — tenant/thread/actor fields included in the hash
- Trace redaction (issue #110) — payloads are sanitized before hashing
- Closed event taxonomy (issue #115) — stable `type` / `actor_type` in the canonical form
