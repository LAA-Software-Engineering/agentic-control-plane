// Package telemetry provides optional OpenTelemetry trace export using gen_ai semantic
// conventions (issue #108). When disabled, callers pay no network cost; SQLite traces
// remain authoritative via [github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace].
package telemetry
