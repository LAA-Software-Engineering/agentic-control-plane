package native

import (
	"context"
	"errors"
	"strings"
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

func TestRegistryDispatchPullRequestFetch(t *testing.T) {
	reg := NewRegistry()
	tests := []struct {
		name    string
		with    map[string]any
		wantKey string
		wantErr string
	}{
		{
			name:    "valid JSON",
			with:    map[string]any{"pr": `{"number":1,"title":"demo"}`},
			wantKey: "pull_request",
		},
		{
			name:    "missing pr",
			with:    map[string]any{},
			wantErr: "requires string field pr",
		},
		{
			name:    "empty pr",
			with:    map[string]any{"pr": "   "},
			wantErr: "requires string field pr",
		},
		{
			name:    "malformed JSON",
			with:    map[string]any{"pr": `{not json`},
			wantErr: "pull_request.fetch pr:",
		},
		{
			name:    "non-string pr",
			with:    map[string]any{"pr": 42},
			wantErr: "requires string field pr",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, _, err := reg.Dispatch(context.Background(), "pull_request.fetch", tt.with)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := out[tt.wantKey]; !ok {
				t.Fatalf("output missing %q: %v", tt.wantKey, out)
			}
		})
	}
}

func TestRegistryDispatchOfflineOperations(t *testing.T) {
	reg := NewRegistry()

	out, _, err := reg.Dispatch(context.Background(), "echo", map[string]any{"x": 1})
	if err != nil {
		t.Fatal(err)
	}
	if echo, ok := out["echo"].(map[string]any); !ok || echo["x"] != 1 {
		t.Fatalf("echo output = %v", out)
	}

	out, _, err = reg.Dispatch(context.Background(), "identity", map[string]any{"value": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if out["value"] != "ok" || out["ok"] != true {
		t.Fatalf("identity output = %v", out)
	}
}
