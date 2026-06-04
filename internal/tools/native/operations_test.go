package native

import "testing"

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

func TestDispatchOperationsMatchCatalog(t *testing.T) {
	t.Helper()
	for _, op := range DispatchOperations {
		if _, ok := operationCatalog[op]; !ok {
			t.Errorf("DispatchOperations %q missing from operationCatalog", op)
		}
	}
	for op := range operationCatalog {
		found := false
		for _, name := range DispatchOperations {
			if name == op {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("operationCatalog %q missing from DispatchOperations", op)
		}
	}
}
