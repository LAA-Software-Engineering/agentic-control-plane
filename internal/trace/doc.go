// Package trace records structured execution events for logs and debugging.
//
// [Recorder] checks that a run row exists before appending (clear failure when StartRun was
// skipped). Event type strings are defined as Event* constants in events.go (design doc §12.2 I).
//
// Before persistence, event payloads pass through sanitize → redact → truncate ([PrepareEventData],
// issue #110). Use [NewRecorderForGraph] so limits and redactKeys come from Project.spec.traces.
package trace
