package native

import (
	"context"
	"errors"
	"testing"
)

func TestOperationKnown(t *testing.T) {
	tests := []struct {
		op   string
		want bool
	}{
		{"echo", true},
		{"identity", true},
		{"run", true},
		{"command.run", true},
		{"delete_records", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := OperationKnown(tt.op); got != tt.want {
			t.Errorf("OperationKnown(%q) = %v want %v", tt.op, got, tt.want)
		}
	}
}

func TestTopLevelArgsForOperation(t *testing.T) {
	args, ok := TopLevelArgsForOperation("identity")
	if !ok || len(args) != 1 || args[0] != "value" {
		t.Fatalf("identity args = %v ok=%v", args, ok)
	}
	_, ok = TopLevelArgsForOperation("echo")
	if !ok {
		t.Fatal("echo should accept arbitrary args")
	}
	_, ok = TopLevelArgsForOperation("missing")
	if ok {
		t.Fatal("missing operation should not have schema")
	}
}

// TestRegistryDispatchMatchesCatalog ensures dispatchHandlers and operationCatalog stay aligned.
// Adding a handler without catalog metadata (or vice versa) must fail CI.
func TestRegistryDispatchMatchesCatalog(t *testing.T) {
	t.Helper()
	for op := range dispatchHandlers {
		if _, ok := operationCatalog[op]; !ok {
			t.Errorf("dispatchHandlers %q missing from operationCatalog", op)
		}
	}
	for op := range operationCatalog {
		if _, ok := dispatchHandlers[op]; !ok {
			t.Errorf("operationCatalog %q missing from dispatchHandlers", op)
		}
	}
}

func TestDispatchOperationsMatchHandlers(t *testing.T) {
	t.Helper()
	if len(DispatchOperations) != len(dispatchHandlers) {
		t.Fatalf("len DispatchOperations=%d want len dispatchHandlers=%d",
			len(DispatchOperations), len(dispatchHandlers))
	}
	for _, op := range DispatchOperations {
		if _, ok := dispatchHandlers[op]; !ok {
			t.Errorf("DispatchOperations %q missing from dispatchHandlers", op)
		}
	}
}

func TestRegistryDispatchUnknownOperation(t *testing.T) {
	reg := NewRegistry()
	_, _, err := reg.Dispatch(context.Background(), "not-a-real-op", nil)
	if err == nil {
		t.Fatal("expected error for unknown operation")
	}
	if !errors.Is(err, ErrUnknownOperation) {
		t.Fatalf("error = %v, want ErrUnknownOperation", err)
	}
}

func TestRegistryDispatchKnownOperationsNotUnknown(t *testing.T) {
	reg := NewRegistry()
	for op := range dispatchHandlers {
		_, _, err := reg.Dispatch(context.Background(), op, map[string]any{})
		if errors.Is(err, ErrUnknownOperation) {
			t.Errorf("Dispatch(%q) returned ErrUnknownOperation; handler must be registered", op)
		}
	}
}
