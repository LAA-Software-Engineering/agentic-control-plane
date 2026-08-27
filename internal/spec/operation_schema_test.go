package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func toolGraphWithOperationSchema(ref string) *ProjectGraph {
	return &ProjectGraph{
		Tools: map[string]*ToolResource{
			"github": {
				APIVersion: APIVersionV0,
				Kind:       KindTool,
				Metadata:   Metadata{Name: "github"},
				Spec: ToolSpec{
					Type:               "native",
					OperationsDeclared: true,
					Operations:         map[string]ToolOperation{"read_pr": {Effects: []string{"github.read"}, Schema: ref}},
				},
			},
		},
	}
}

func TestValidateProjectGraph_operationSchemaMustResolve(t *testing.T) {
	root := t.TempDir()
	// A declared operation schema that does not exist is a validation error naming the operation.
	err := ValidateProjectGraph(toolGraphWithOperationSchema("./schemas/missing.json"), root)
	if err == nil || !strings.Contains(err.Error(), "spec.operations[\"read_pr\"].schema") {
		t.Fatalf("expected an operation-schema validation error, got %v", err)
	}
}

func TestParseResourceFromBytes_operationSchemaAndPos(t *testing.T) {
	const y = `apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: github
spec:
  type: native
  operations:
    read_pr:
      effects: [github.read]
      schema: ./schemas/read_pr.json
`
	dec, err := ParseResourceFromBytes([]byte(y), "github.yaml")
	if err != nil {
		t.Fatal(err)
	}
	op := dec.Resource.(*ToolResource).Spec.Operations["read_pr"]
	if op.Schema != "./schemas/read_pr.json" {
		t.Fatalf("operation schema not decoded from YAML: %q", op.Schema)
	}
	if op.SchemaPos.Line != 10 {
		t.Fatalf("SchemaPos should point at the schema: node (line 10), got %#v", op.SchemaPos)
	}
}

func TestValidateProjectGraph_operationSchemaUncompilable(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "schemas"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Present and valid JSON, but not a valid JSON Schema (type must be a string/array of strings).
	if err := os.WriteFile(filepath.Join(root, "schemas", "bad.json"), []byte(`{"type":123}`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ValidateProjectGraph(toolGraphWithOperationSchema("./schemas/bad.json"), root)
	if err == nil || !strings.Contains(err.Error(), "spec.operations[\"read_pr\"].schema") {
		t.Fatalf("an uncompilable operation schema must fail validate, got %v", err)
	}
}

func TestValidateProjectGraph_operationSchemaValidPasses(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "schemas"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "schemas", "read_pr.json"), []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProjectGraph(toolGraphWithOperationSchema("./schemas/read_pr.json"), root); err != nil {
		t.Fatalf("a resolvable operation schema must validate: %v", err)
	}
}
