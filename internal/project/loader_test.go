package project

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProject_duplicateKindName(t *testing.T) {
	root := filepath.Join("testdata", "dup_agents")
	_, err := LoadProject(root)
	if err == nil {
		t.Fatal("expected duplicate Agent/foo error")
	}
	var dup *DuplicateResourceError
	if !errors.As(err, &dup) {
		t.Fatalf("expected *DuplicateResourceError, got %T: %v", err, err)
	}
	if dup.Kind != "Agent" || dup.Name != "foo" {
		t.Fatalf("duplicate = %s/%s, want Agent/foo", dup.Kind, dup.Name)
	}
	if len(dup.Paths) != 2 {
		t.Fatalf("Paths = %v, want two entries", dup.Paths)
	}
	has := func(suffix string) bool {
		for _, p := range dup.Paths {
			if strings.HasSuffix(filepath.ToSlash(p), suffix) {
				return true
			}
		}
		return false
	}
	if !has("agents/one.yaml") || !has("agents/two.yaml") {
		t.Fatalf("expected paths to include agents/one.yaml and agents/two.yaml, got %#v", dup.Paths)
	}
}

func TestLoadProject_nestedImportDirectory(t *testing.T) {
	root := filepath.Join("testdata", "nested_import")
	g, err := LoadProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if g.Meta.Name != "nested-test" {
		t.Fatalf("project name = %q", g.Meta.Name)
	}
	a, ok := g.Agents["deep-agent"]
	if !ok || a == nil {
		t.Fatalf("expected Agent deep-agent from nested/deep/here.yaml, got agents=%v", keys(g.Agents))
	}
	if a.Metadata.Name != "deep-agent" {
		t.Fatalf("agent metadata.name = %q", a.Metadata.Name)
	}
}

func TestLoadProject_minimalNoImports(t *testing.T) {
	root := filepath.Join("testdata", "minimal")
	g, err := LoadProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if g.Meta.Name != "minimal" {
		t.Fatalf("Meta.Name = %q", g.Meta.Name)
	}
	if len(g.Agents) != 0 {
		t.Fatalf("expected no agents, got %d", len(g.Agents))
	}
}

func keys[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
