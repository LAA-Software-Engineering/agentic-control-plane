package state

import (
	"errors"
	"fmt"
	"strings"
)

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

// ErrAttributionRequired is returned when explicit tenant, thread, and actor ids are required
// but one or more are omitted (issue #111 production guardrail).
var ErrAttributionRequired = errors.New("attribution required: set tenant_id, thread_id, and actor_id")

// UsesAttributionDefaults reports whether any core attribution field is unset and would receive
// a local default from [NormalizeAttribution].
func UsesAttributionDefaults(a RunAttribution) bool {
	return strings.TrimSpace(a.TenantID) == "" ||
		strings.TrimSpace(a.ThreadID) == "" ||
		strings.TrimSpace(a.ActorID) == ""
}

// RequireExplicitAttribution returns [ErrAttributionRequired] when tenant_id, thread_id, or
// actor_id is empty. Call before [NormalizeAttribution] when production guardrails are enabled.
func RequireExplicitAttribution(a RunAttribution) error {
	var missing []string
	if strings.TrimSpace(a.TenantID) == "" {
		missing = append(missing, "tenant_id")
	}
	if strings.TrimSpace(a.ThreadID) == "" {
		missing = append(missing, "thread_id")
	}
	if strings.TrimSpace(a.ActorID) == "" {
		missing = append(missing, "actor_id")
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%w (missing: %s)", ErrAttributionRequired, strings.Join(missing, ", "))
}

// AttributionFromRun copies persisted attribution from a run row.
func AttributionFromRun(r *Run) RunAttribution {
	if r == nil {
		return RunAttribution{}
	}
	return RunAttribution{
		TenantID:       r.TenantID,
		ThreadID:       r.ThreadID,
		ActorID:        r.ActorID,
		ParentRunID:    r.ParentRunID,
		RequestID:      r.RequestID,
		IdempotencyKey: r.IdempotencyKey,
		Source:         r.Source,
	}
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
