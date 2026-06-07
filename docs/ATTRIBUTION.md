# Run attribution (tenant, thread, actor)

Issue [#111](https://github.com/LAA-Software-Engineering/agentic-control-plane/issues/111) adds lightweight tenancy and attribution to `runs` and `trace_events`.

## Fields

| Field | Purpose |
| --- | --- |
| `tenant_id` | Outermost multi-tenant scope |
| `thread_id` | Session continuity across runs and `--resume` |
| `actor_id` | Who triggered the run (caller-asserted for now) |
| `parent_run_id` | Lineage for sub-runs (not set on resume of the same run) |
| `request_id` | Per-invocation correlation id (distinct from `run_id`) |
| `idempotency_key` | Client reference key (stored only; dedupe is not enforced yet) |
| `source` | Origin label (`cli`, `actions`, `api`, …) |

Trace events duplicate `tenant_id`, `thread_id`, and `actor_id` from the parent run so `logs` and the inspector can filter without joins. These fields are included in the tamper-evident trace hash chain (issue #116); see [AUDIT_CHAIN.md](./AUDIT_CHAIN.md).

## CLI defaults (local only)

When flags are omitted, `agentctl run` stores:

- `tenant_id`: `tenant-1`
- `thread_id`: `thread-1`
- `actor_id`: `user-1`
- `source`: `cli`

**Do not rely on these defaults in CI or production.** A stderr warning is emitted when defaults apply. For CI/prod, pass real actor ids, set env vars, or enable the guardrail:

```bash
export AGENTCTL_REQUIRE_ATTRIBUTION=1
# or: agentctl run ... --require-attribution --tenant-id ... --thread-id ... --actor-id ...
```

Env overrides when flags are omitted: `AGENTCTL_TENANT_ID`, `AGENTCTL_THREAD_ID`, `AGENTCTL_ACTOR_ID`.

```bash
agentctl run workflow/demo \
  --tenant-id acme \
  --thread-id prod-review-42 \
  --actor-id github-actions@acme
```

Filter history:

```bash
agentctl logs --tenant-id acme --thread-id prod-review-42
```

## Resume

`agentctl run --resume <run-id>` reuses the original `run_id` and `thread_id` from the persisted run row. Attribution flags on resume are ignored so thread timelines stay coherent. `--parent-run-id` is for genuine sub-runs, not resumes.

## Inspector API

`GET /api/runs` accepts optional query parameters:

- `tenant_id`
- `thread_id`
- `actor_id`
- `workflow`
- `limit`

## OpenTelemetry

When telemetry is enabled, spans emit `gen_ai.tenant.id`, `gen_ai.thread.id`, `gen_ai.actor.id`, `gen_ai.run.id`, and `gen_ai.request.id` alongside existing gen_ai attributes. See [OTEL.md](./OTEL.md).

## request_id

When omitted, [state.RuntimeStore.StartRun] assigns a new UUID via `util.NewRequestID()`. Legacy rows migrated from pre-005 databases may have `request_id == run_id`.

## Idempotency key

`idempotency_key` is persisted and exposed in JSON for future dedupe. There is no unique index or at-most-once enforcement in this release — do not assume idempotent execution from the key alone.

## Production guidance

- SQLite attribution is advisory; DB-level tenant isolation belongs to a future remote/Postgres store.
- `actor_id` is supplied by the caller and is not authenticated in this release. Do not use attribution for access control.
