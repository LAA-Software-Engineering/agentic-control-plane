package catalog

import (
	"strings"
	"testing"
)

func TestIsKnown_localBuiltin(t *testing.T) {
	if !IsKnown(NameLocal) {
		t.Fatal("local should be known")
	}
	if !IsKnown("") {
		t.Fatal("empty runtime should be implicit local")
	}
	if IsKnown("missing") {
		t.Fatal("missing runtime should not be known")
	}
}

func TestKnownNames_includesLocal(t *testing.T) {
	names := KnownNames()
	if len(names) == 0 {
		t.Fatal("expected names")
	}
	found := false
	for _, n := range names {
		if n == NameLocal {
			found = true
		}
	}
	if !found {
		t.Fatalf("KnownNames() = %v", names)
	}
}

func TestRegister_idempotent(t *testing.T) {
	const name = "catalog-test-idempotent"
	Register(name)
	Register(name)
	if !IsKnown(name) {
		t.Fatal("expected known")
	}
	mu.Lock()
	delete(names, name)
	mu.Unlock()
}

func TestErrUnknownRuntime_message(t *testing.T) {
	err := (&ErrUnknownRuntime{Name: "edge"}).Error()
	if !strings.Contains(err, "edge") || !strings.Contains(err, NameLocal) {
		t.Fatalf("error = %q", err)
	}
}
