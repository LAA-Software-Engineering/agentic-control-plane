package claudecode

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/runtime"
)

func TestRegistered(t *testing.T) {
	if !runtime.IsKnown(Name) {
		t.Fatalf("%q should be a known runtime", Name)
	}
	factory, err := runtime.Lookup(Name)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", Name, err)
	}
	if _, err := factory(runtime.Deps{}); err != nil {
		t.Fatalf("factory: %v", err)
	}
}

func TestStubNotImplemented(t *testing.T) {
	r, err := NewFromDeps(runtime.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Invoke(context.Background(), nil, runtime.InvokeOptions{}); !errors.Is(err, errPendingIntegration) {
		t.Fatalf("Invoke should be not-implemented, got %v", err)
	}
	if _, err := r.Resume(context.Background(), nil, runtime.ResumeOptions{}); !errors.Is(err, errPendingIntegration) {
		t.Fatalf("Resume should be not-implemented, got %v", err)
	}
	if h := r.Health(context.Background()); h.State != runtime.HealthDegraded {
		t.Fatalf("stub Health should be degraded, got %q", h.State)
	}
}

// TestErrorNamesFollowUp keeps the not-implemented error pointing at the adapter issue so a user
// who selects the runtime early gets a clear signpost.
func TestErrorNamesFollowUp(t *testing.T) {
	if !strings.Contains(errPendingIntegration.Error(), "#338") {
		t.Fatalf("not-implemented error should reference #338: %v", errPendingIntegration)
	}
}
