package state

import "strings"

// Local development defaults for run attribution (issue #111).
// Do not rely on these in CI or production; pass explicit tenant, thread, and actor identifiers.
const (
	DefaultTenantID = "tenant-1"
	DefaultThreadID = "thread-1"
	DefaultActorID  = "user-1"
	DefaultSource   = "cli"
)

// RunAttribution scopes a run to a tenant and thread and records who triggered it.
type RunAttribution struct {
	TenantID       string
	ThreadID       string
	ActorID        string
	ParentRunID    string
	RequestID      string
	IdempotencyKey string
	Source         string
}

// NormalizeAttribution fills empty attribution fields with [DefaultTenantID], [DefaultThreadID],
// [DefaultActorID], and [DefaultSource]. Optional fields (parent run, idempotency key) stay empty
// when unset.
func NormalizeAttribution(a *RunAttribution) {
	if a == nil {
		return
	}
	if strings.TrimSpace(a.TenantID) == "" {
		a.TenantID = DefaultTenantID
	}
	if strings.TrimSpace(a.ThreadID) == "" {
		a.ThreadID = DefaultThreadID
	}
	if strings.TrimSpace(a.ActorID) == "" {
		a.ActorID = DefaultActorID
	}
	if strings.TrimSpace(a.Source) == "" {
		a.Source = DefaultSource
	}
	a.TenantID = strings.TrimSpace(a.TenantID)
	a.ThreadID = strings.TrimSpace(a.ThreadID)
	a.ActorID = strings.TrimSpace(a.ActorID)
	a.ParentRunID = strings.TrimSpace(a.ParentRunID)
	a.RequestID = strings.TrimSpace(a.RequestID)
	a.IdempotencyKey = strings.TrimSpace(a.IdempotencyKey)
	a.Source = strings.TrimSpace(a.Source)
}

// ApplyAttribution copies normalized attribution onto a [Run].
func ApplyAttribution(r *Run, a RunAttribution) {
	if r == nil {
		return
	}
	NormalizeAttribution(&a)
	r.TenantID = a.TenantID
	r.ThreadID = a.ThreadID
	r.ActorID = a.ActorID
	r.ParentRunID = a.ParentRunID
	r.RequestID = a.RequestID
	r.IdempotencyKey = a.IdempotencyKey
	r.Source = a.Source
}
