# OpenTelemetry trace export (issue #108)

ACP stores structured traces in SQLite by default. Optional OTLP export emits `gen_ai.*` spans for runs, model calls, tool executions, and HITL approvals so you can view them in Jaeger, Grafana Tempo, or any OTLP backend.

## Project configuration

Add `spec.telemetry` to `project.yaml`:

```yaml
spec:
  telemetry:
    enabled: true
    serviceName: my-agent-system
    endpoint: env:OTEL_EXPORTER_OTLP_ENDPOINT
    consoleExport: false
```

| Field | Description |
|-------|-------------|
| `enabled` | When `false` (default), no OTLP exporters are started and there is no network overhead. |
| `serviceName` | Required when enabled; becomes the OpenTelemetry service name. |
| `endpoint` | OTLP HTTP endpoint URL or `env:VAR` (same token style as `apiKeyFrom`). |
| `consoleExport` | Pretty-print spans to stderr (useful for local debugging). |

At least one of `endpoint` or `consoleExport` must be set when telemetry is enabled.

If the exporter cannot be initialized, `agentctl` logs a warning and the run still completes; SQLite traces are unchanged.

## Span tree

```
agent.run (root)
 ├─ model.chat
 ├─ tool.exec        (gen_ai.tool.* safety attrs)
 └─ approval         (HITL gate)
```

Resume after interrupt links the new `agent.run` span to the interrupted one via an OpenTelemetry span link (`gen_ai.hitl.resumed=true`).

## Local Jaeger (copy-paste)

Start Jaeger all-in-one with OTLP HTTP enabled:

```bash
docker run -d --name jaeger \
  -p 16686:16686 \
  -p 4318:4318 \
  -e COLLECTOR_OTLP_ENABLED=true \
  jaegertracing/all-in-one:1.64
```

Export endpoint for ACP:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318/v1/traces
```

Example `project.yaml` fragment:

```yaml
spec:
  telemetry:
    enabled: true
    serviceName: demo
    endpoint: env:OTEL_EXPORTER_OTLP_ENDPOINT
```

Run a workflow, then open [http://localhost:16686](http://localhost:16686) and search for service `demo`.

## Attributes (WayFind-aligned)

Core run attributes include `gen_ai.system` (`agentctl`), `gen_ai.operation.name`, `gen_ai.run.id`, `gen_ai.workflow`, and `gen_ai.agent.version`.

Tool spans include `gen_ai.tool.trusted`, `gen_ai.tool.side_effects`, and `gen_ai.tool.requires_approval` from resolved Tool safety metadata.

HITL uses `gen_ai.hitl.interrupted` and `gen_ai.hitl.resumed` on the root run span.
