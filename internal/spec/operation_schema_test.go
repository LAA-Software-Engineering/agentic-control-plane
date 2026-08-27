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
