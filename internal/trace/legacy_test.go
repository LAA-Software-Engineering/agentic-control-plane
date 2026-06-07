package trace

import "testing"

// migration006Expectations mirrors migrations/sqlite/006_trace_event_taxonomy.sql UPDATE rules.
func TestLegacyEventTypeMap_matchesMigration006Expectations(t *testing.T) {
	expect := map[string]struct {
		canonical EventType
		actor     ActorType
	}{
		"run.started":        {EventRunStarted, ActorAgent},
		"run.finished":       {EventRunFinished, ActorAgent},
		"run.interrupted":    {EventRunError, ActorSystem},
		"run.resumed":        {EventRunStarted, ActorAgent},
		"step.started":       {EventToolSelection, ActorAgent},
		"step.finished":      {EventToolExecution, ActorAgent},
		"step.failed":        {EventRunError, ActorSystem},
		"tool.called":        {EventToolSelection, ActorAgent},
		"tool.completed":     {EventToolExecution, ActorAgent},
		"model.called":       {EventLLMCompletion, ActorAgent},
		"model.completed":    {EventLLMCompletion, ActorAgent},
		"policy.denied":      {EventSystemError, ActorSystem},
		"approval.requested": {EventHitlRequestCreated, ActorSystem},
		"approval.resolved":  {EventHitlDecisionSubmitted, ActorUser},
	}

	got := LegacyEventTypeMappings()
	if len(got) != len(expect) {
		t.Fatalf("map size = %d want %d", len(got), len(expect))
	}
	for legacy, want := range expect {
		canonical, ok := got[legacy]
		if !ok {
			t.Fatalf("missing legacy key %q", legacy)
		}
		if EventType(canonical) != want.canonical {
			t.Fatalf("%q: canonical = %q want %q", legacy, canonical, want.canonical)
		}
		if LegacyActorTypeForEvent(canonical) != want.actor {
			t.Fatalf("%q: actor = %q want %q", legacy, LegacyActorTypeForEvent(canonical), want.actor)
		}
	}
}
