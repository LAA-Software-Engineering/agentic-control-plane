// Package telemetry provides optional OpenTelemetry trace export using gen_ai semantic
// conventions (issue #108). When disabled, callers pay no network cost; SQLite traces
// remain authoritative via [github.com/Terfyn/terfyn/internal/trace].
package telemetry
