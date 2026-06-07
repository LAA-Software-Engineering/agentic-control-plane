// Package trace records structured execution events for logs and debugging.
//
// [Recorder] checks that a run row exists before appending (clear failure when StartRun was
// skipped). Event and actor types are closed enums in taxonomy.go ([EventType], [ActorType],
// [TaxonomyVersion]; issue #115). Loaders normalize legacy dot-notation type strings on read.
//
// Before persistence, event payloads pass through sanitize → redact → truncate ([PrepareEventData],
// issue #110). Use [NewRecorderForGraph] so limits and redactKeys come from Project.spec.traces.
package trace
