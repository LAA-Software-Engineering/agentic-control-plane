// Package inspect provides a read-only local HTTP server for browsing SQLite
// deployment and runtime state (runs, trace events, applied resources, checkpoints).
//
// It is intended for local exploration via agentctl inspect --web and does not expose
// mutation endpoints.
package inspect
