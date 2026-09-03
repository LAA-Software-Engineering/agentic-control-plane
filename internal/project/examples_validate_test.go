package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Terfyn/terfyn/internal/spec"
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
		// An example is loadable if it has a project.yaml OR any .agent source (a .agent-only project,
		// issue #430/#440). Skip a directory with neither (nothing to load).
		if !hasProjectFile(dir) && !hasAgentFile(t, dir) {
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

func hasProjectFile(dir string) bool {
	for _, n := range []string{"project.yaml", "project.yml"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			return true
		}
	}
	return false
}

func hasAgentFile(t *testing.T, dir string) bool {
	t.Helper()
	found := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if !d.IsDir() && filepath.Ext(path) == ".agent" {
			found = true
		}
		return nil
	})
	return found
}
