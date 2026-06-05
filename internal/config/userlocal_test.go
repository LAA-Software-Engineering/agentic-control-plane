package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func writeYAML(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverUserLocalPaths_order(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	writeYAML(t, filepath.Join(home, ".config", "agentctl", "config.yaml"), "defaults:\n  model: global\n")
	writeYAML(t, filepath.Join(root, ".agentic", "local.yaml"), "defaults:\n  model: project-local\n")

	got := DiscoverUserLocalPaths(root, home)
	if len(got) != 2 {
		t.Fatalf("paths = %v, want 2", got)
	}
	if !strings.HasSuffix(got[0], "agentctl/config.yaml") {
		t.Fatalf("global should be first: %v", got)
	}
	if !strings.HasSuffix(got[1], ".agentic/local.yaml") {
		t.Fatalf("project-local should be second: %v", got)
	}
}

func TestLoadUserLocalOverlay_unknownField(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "local.yaml")
	writeYAML(t, p, "defualts:\n  model: x\n")
	_, err := LoadUserLocalOverlay(p)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "defualts") {
		t.Fatalf("want field in error: %v", err)
	}
	if !strings.Contains(err.Error(), "defaults") {
		t.Fatalf("want suggestion: %v", err)
	}
}

func TestMergeUserLocalOverlays_precedence(t *testing.T) {
	global := &UserLocalOverlay{Defaults: &spec.ProjectDefaults{Model: "global"}}
	local := &UserLocalOverlay{Defaults: &spec.ProjectDefaults{Model: "project-local", Policy: "strict"}}
	merged := MergeUserLocalOverlays(global, local)
	if merged.Defaults.Model != "project-local" {
		t.Fatalf("model = %q, want project-local", merged.Defaults.Model)
	}
	if merged.Defaults.Policy != "strict" {
		t.Fatalf("policy = %q, want strict", merged.Defaults.Policy)
	}
}

func TestApplyUserLocalUnder_projectWins(t *testing.T) {
	project := &spec.ProjectSpec{
		Defaults: &spec.ProjectDefaults{Model: "project"},
		State:    &spec.ProjectStateConfig{DSN: "project.db"},
	}
	userLocal := &UserLocalOverlay{
		Defaults: &spec.ProjectDefaults{Model: "local", Runtime: "remote"},
		State:    &spec.ProjectStateConfig{DSN: "local.db", Backend: "sqlite"},
	}
	ApplyUserLocalUnder(project, userLocal)
	if project.Defaults.Model != "project" {
		t.Fatalf("project model should win, got %q", project.Defaults.Model)
	}
	if project.Defaults.Runtime != "remote" {
		t.Fatalf("unset project field should come from user-local, got %q", project.Defaults.Runtime)
	}
	if project.State.DSN != "project.db" {
		t.Fatalf("project state dsn should win, got %q", project.State.DSN)
	}
	if project.State.Backend != "sqlite" {
		t.Fatalf("unset backend should come from user-local, got %q", project.State.Backend)
	}
}
