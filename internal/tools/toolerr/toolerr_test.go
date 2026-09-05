package toolerr

import (
	"errors"
	"fmt"
	"testing"
)

func TestRecoverable_IsMarkedAndUnwraps(t *testing.T) {
	cause := errors.New("http 401 api_key=sk-live-SECRET")
	err := Recoverable(`read_file: "x.go" does not exist`, cause)

	if !errors.Is(err, ErrRecoverable) {
		t.Fatal("Recoverable must match ErrRecoverable")
	}
	if !errors.Is(err, cause) {
		t.Fatal("Recoverable must unwrap to its cause for the audit/log path")
	}
	// The marker survives an outer wrap, so a caller can add context and stay recoverable.
	wrapped := fmt.Errorf("engine: %w", err)
	if !errors.Is(wrapped, ErrRecoverable) {
		t.Fatal("ErrRecoverable must survive an outer %w wrap")
	}
}

func TestSafeObservation_ReturnsConstructedNotRaw(t *testing.T) {
	cause := errors.New("http 401 api_key=sk-live-SECRET password=hunter2")
	err := Recoverable(`read_file: "x.go" does not exist`, cause)

	msg, ok := SafeObservation(err)
	if !ok {
		t.Fatal("a recoverable error must report ok=true")
	}
	if msg != `read_file: "x.go" does not exist` {
		t.Fatalf("observation = %q, want the constructed message", msg)
	}
	for _, secret := range []string{"sk-live-SECRET", "hunter2", "http 401"} {
		if contains(msg, secret) {
			t.Fatalf("observation leaked %q from the underlying cause: %q", secret, msg)
		}
	}
}

func TestSafeObservation_EmptyMessageFallsBackToToken(t *testing.T) {
	msg, ok := SafeObservation(Recoverable("", errors.New("secret detail")))
	if !ok || msg != "tool call failed" {
		t.Fatalf("empty observation must fall back to the generic token, got %q ok=%v", msg, ok)
	}
	if contains(msg, "secret") {
		t.Fatalf("fallback must not expose the cause: %q", msg)
	}
}

func TestSafeObservation_UnmarkedErrorIsNotRecoverable(t *testing.T) {
	if msg, ok := SafeObservation(errors.New("plain adapter diagnostic")); ok || msg != "" {
		t.Fatalf("an unmarked error must not be recoverable, got %q ok=%v", msg, ok)
	}
	if SafeObservationOK(nil) {
		t.Fatal("nil is not recoverable")
	}
}

// SafeObservationOK is a tiny helper kept in the test to document the nil case.
func SafeObservationOK(err error) bool {
	_, ok := SafeObservation(err)
	return ok
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
