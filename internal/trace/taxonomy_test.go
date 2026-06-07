package trace

import (
	"strings"
	"testing"
)

func TestEventType_Known(t *testing.T) {
	for _, et := range AllEventTypes() {
		if !et.Known() {
			t.Errorf("%q should be known", et)
		}
	}
	if EventType("future_event").Known() {
		t.Fatal("unknown type should not be known")
	}
}

func TestParseEventType_legacyAndForwardCompat(t *testing.T) {
	tests := []struct {
		in     string
		want   EventType
		known  bool
		normal bool
	}{
		{"run.started", EventRunStarted, true, true},
		{"run_started", EventRunStarted, true, true},
		{"tool.called", EventToolSelection, true, true},
		{"future_event", EventType("future_event"), false, false},
		{"", "", false, false},
	}
	for _, tc := range tests {
		got, known := ParseEventType(tc.in)
		if got != tc.want || known != tc.known {
			t.Errorf("ParseEventType(%q) = (%q, %v), want (%q, %v)", tc.in, got, known, tc.want, tc.known)
		}
	}
}

func TestValidateEventType(t *testing.T) {
	if err := ValidateEventType(EventRunStarted); err != nil {
		t.Fatal(err)
	}
	if err := ValidateEventType(EventType("bogus")); err == nil {
		t.Fatal("expected error for bogus type")
	} else if !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("expected type in error: %v", err)
	}
}

func TestValidateActorType(t *testing.T) {
	for _, at := range AllActorTypes() {
		if err := ValidateActorType(at); err != nil {
			t.Errorf("%q: %v", at, err)
		}
	}
	if err := ValidateActorType(ActorType("bot")); err == nil {
		t.Fatal("expected error")
	}
}

func TestNormalizeStoredEventType(t *testing.T) {
	if got := NormalizeStoredEventType("run.started"); got != string(EventRunStarted) {
		t.Fatalf("got %q", got)
	}
	if got := NormalizeStoredEventType("custom"); got != "custom" {
		t.Fatalf("got %q", got)
	}
}

func TestEventType_OTelMapping(t *testing.T) {
	if EventToolExecution.SpanName() != "tool.exec" {
		t.Fatalf("span = %q", EventToolExecution.SpanName())
	}
	if EventLLMCompletion.GenAIOperation() != "model.chat" {
		t.Fatalf("op = %q", EventLLMCompletion.GenAIOperation())
	}
	if EventHitlRequestCreated.TimelineGroup() != "hitl" {
		t.Fatalf("group = %q", EventHitlRequestCreated.TimelineGroup())
	}
	if EventRunStarted.TimelineIcon() == "" {
		t.Fatal("expected icon")
	}
}

func TestAllEventTypeStrings_sorted(t *testing.T) {
	got := AllEventTypeStrings()
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("not sorted: %v", got)
		}
	}
}

func TestLegacyActorTypeForEvent(t *testing.T) {
	if LegacyActorTypeForEvent(string(EventHitlDecisionSubmitted)) != ActorUser {
		t.Fatal("hitl decision should be user")
	}
	if LegacyActorTypeForEvent(string(EventSystemError)) != ActorSystem {
		t.Fatal("system error should be system")
	}
}
