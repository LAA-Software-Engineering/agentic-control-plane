package state

import "testing"

func TestNormalizeAttribution_defaults(t *testing.T) {
	a := RunAttribution{}
	NormalizeAttribution(&a)
	if a.TenantID != DefaultTenantID || a.ThreadID != DefaultThreadID || a.ActorID != DefaultActorID || a.Source != DefaultSource {
		t.Fatalf("defaults: %+v", a)
	}
}

func TestNormalizeAttribution_preservesExplicit(t *testing.T) {
	a := RunAttribution{
		TenantID:       " acme ",
		ThreadID:       "prod-thread",
		ActorID:        "ci-bot",
		ParentRunID:    " parent-1 ",
		RequestID:      "req-1",
		IdempotencyKey: "idem-1",
		Source:         "actions",
	}
	NormalizeAttribution(&a)
	if a.TenantID != "acme" || a.ThreadID != "prod-thread" || a.ActorID != "ci-bot" {
		t.Fatalf("trimmed: %+v", a)
	}
	if a.ParentRunID != "parent-1" || a.RequestID != "req-1" || a.IdempotencyKey != "idem-1" || a.Source != "actions" {
		t.Fatalf("optional: %+v", a)
	}
}

func TestNormalizeAttribution_nilSafe(t *testing.T) {
	NormalizeAttribution(nil)
}

func TestApplyAttribution(t *testing.T) {
	var r Run
	ApplyAttribution(&r, RunAttribution{TenantID: "t", ThreadID: "th", ActorID: "a", RequestID: "r1", Source: "api"})
	if r.TenantID != "t" || r.ThreadID != "th" || r.ActorID != "a" || r.RequestID != "r1" || r.Source != "api" {
		t.Fatalf("run: %+v", r)
	}
}

func TestApplyAttribution_nilRun(t *testing.T) {
	ApplyAttribution(nil, RunAttribution{})
}
