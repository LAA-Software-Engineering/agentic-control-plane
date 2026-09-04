package project

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/spec"
)

func TestLoadProject_duplicateKindName(t *testing.T) {
	root := filepath.Join("testdata", "dup_agents")
	_, err := LoadProjectAllowingYAML(root)
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
	g, err := LoadProjectAllowingYAML(root)
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
	g, err := LoadProjectAllowingYAML(root)
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

// TestLoadProject_agentOnly is issue #430: a project with only .agent source and no project.yaml
// loads — the loader synthesizes a Project (name from the directory basename) and folds in the
// checked .agent resources.
func TestLoadProject_agentOnly(t *testing.T) {
	root := filepath.Join("testdata", "agent_only")
	g, execs, err := LoadProjectWithExecutablesAllowingYAML(root)
	if err != nil {
		t.Fatalf("agent-only project must load without project.yaml: %v", err)
	}
	if g.Meta.Name != "agent_only" {
		t.Fatalf("synthesized project name = %q, want the directory basename %q", g.Meta.Name, "agent_only")
	}
	if len(g.Agents) != 1 || g.Agents["assistant"] == nil {
		t.Fatalf("want the .agent-declared assistant agent, got %v", keys(g.Agents))
	}
	if g.Workflows["hello"] == nil {
		t.Fatalf("want the hello workflow, got %v", keys(g.Workflows))
	}
	if execs["hello"] == nil {
		t.Fatalf("want an execution IR for hello, got %v", keys(execs))
	}
	if err := spec.ValidateProjectGraph(g, root); err != nil {
		t.Fatalf("agent-only graph must validate: %v", err)
	}
}

// TestLoadProject_noSourceAtAll: a directory with neither project.yaml nor .agent files is an error
// (nothing to load), preserving the original "no project.yaml" diagnostic.
func TestLoadProject_noSourceAtAll(t *testing.T) {
	root := t.TempDir()
	_, err := LoadProjectAllowingYAML(root)
	if err == nil {
		t.Fatal("empty directory must not load")
	}
	if !strings.Contains(err.Error(), "no project.yaml") {
		t.Fatalf("want the no-project-file error, got %v", err)
	}
}

func keys[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
