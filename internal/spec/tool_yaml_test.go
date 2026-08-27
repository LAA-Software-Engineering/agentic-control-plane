package spec

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A declared-but-empty capability manifest (operations: {}) must survive YAML emission, or export →
// load reopens the callable set (PR #251 review). Operations is yaml:"omitempty" and the presence
// bit is not a YAML field, so ToolSpec.MarshalYAML emits an explicit empty mapping.
func TestToolSpecMarshalYAML_closedEmptyEmitsOperations(t *testing.T) {
	locked := ToolResource{
		APIVersion: APIVersionV0,
		Kind:       KindTool,
		Metadata:   Metadata{Name: "locked"},
		Spec:       ToolSpec{Type: "native", OperationsDeclared: true, Operations: map[string]ToolOperation{}},
	}
	out, err := yaml.Marshal(locked)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "operations:") {
		t.Fatalf("closed-empty tool must emit an operations key:\n%s", out)
	}

	// Reload the emitted YAML: the operations key sets the presence bit again.
	d, err := ParseResourceFromBytes(out, "locked.yaml")
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	tr := d.Resource.(*ToolResource)
	if !tr.Spec.OperationsDeclared {
		t.Fatalf("reloaded closed-empty tool lost its presence bit:\n%s", out)
	}
}

// An open tool (no operations key) must not gain one — the marshaler only ever adds the empty
// mapping for the closed-empty world, never for the common backward-compatible open tool.
func TestToolSpecMarshalYAML_openToolOmitsOperations(t *testing.T) {
	open := ToolResource{
		APIVersion: APIVersionV0,
		Kind:       KindTool,
		Metadata:   Metadata{Name: "open"},
		Spec:       ToolSpec{Type: "native"},
	}
	out, err := yaml.Marshal(open)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), "operations:") {
		t.Fatalf("open tool must not emit an operations key:\n%s", out)
	}
}

// A non-empty manifest marshals its operations normally (the marshaler is a no-op there).
func TestToolSpecMarshalYAML_nonEmptyManifestUnchanged(t *testing.T) {
	tr := ToolResource{
		APIVersion: APIVersionV0,
		Kind:       KindTool,
		Metadata:   Metadata{Name: "github"},
		Spec: ToolSpec{
			Type:               "native",
			OperationsDeclared: true,
			Operations:         map[string]ToolOperation{"read_pr": {Effects: []string{"github.read"}}},
		},
	}
	out, err := yaml.Marshal(tr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "read_pr") {
		t.Fatalf("non-empty manifest must emit its operations:\n%s", out)
	}
}
