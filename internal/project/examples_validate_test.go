package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// TestExamples_validateTypedWiring is the #193 sweep: existing examples still validate
// after schemas are loaded onto the graph and interpolation wiring is checked.
func TestExamples_validateTypedWiring(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "project.yaml")); err != nil {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			g, err := LoadProject(dir)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			spec.NormalizeProjectGraph(g)
			if err := spec.ValidateProjectGraph(g, dir); err != nil {
				t.Fatalf("validate: %v", err)
			}
		})
	}
}
