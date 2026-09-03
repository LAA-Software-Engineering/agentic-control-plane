package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/spec"
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
	writeYAML(t, filepath.Join(home, ".config", "terfyn", "config.yaml"), "defaults:\n  model: global\n")
	writeYAML(t, filepath.Join(root, ".agentic", "local.yaml"), "defaults:\n  model: project-local\n")

	got := DiscoverUserLocalPaths(root, home)
	if len(got) != 2 {
		t.Fatalf("paths = %v, want 2", got)
	}
	if !strings.HasSuffix(got[0], filepath.Join("terfyn", "config.yaml")) {
		t.Fatalf("global should be first: %v", got)
	}
	if !strings.HasSuffix(got[1], filepath.Join(".agentic", "local.yaml")) {
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

func TestDiscoverUserLocalPaths_xdgConfigHome(t *testing.T) {
	root := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeYAML(t, filepath.Join(xdg, "terfyn", "config.yaml"), "defaults:\n  model: xdg\n")

	got := DiscoverUserLocalPaths(root, "")
	if len(got) != 1 {
		t.Fatalf("paths = %v, want 1", got)
	}
	if got[0] != filepath.Join(xdg, "terfyn", "config.yaml") {
		t.Fatalf("path = %q, want XDG path", got[0])
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

func TestApplyUserLocalUnder_limitsPrecedence(t *testing.T) {
	project := &spec.ProjectSpec{
		Limits: &spec.ExecutionLimits{MaxToolOutputBytes: 1000},
	}
	userLocal := &UserLocalOverlay{
		Limits: &spec.ExecutionLimits{
			MaxToolInputBytes:  500,
			MaxToolOutputBytes: 2000,
			MaxCheckpointBytes: 3000,
		},
	}
	ApplyUserLocalUnder(project, userLocal)
	if project.Limits.MaxToolOutputBytes != 1000 {
		t.Fatalf("project output limit should win, got %d", project.Limits.MaxToolOutputBytes)
	}
	if project.Limits.MaxToolInputBytes != 500 {
		t.Fatalf("unset input limit should come from user-local, got %d", project.Limits.MaxToolInputBytes)
	}
	if project.Limits.MaxCheckpointBytes != 3000 {
		t.Fatalf("unset checkpoint limit should come from user-local, got %d", project.Limits.MaxCheckpointBytes)
	}
}

// TestApplyUserLocalUnder_nestingAndLoopFillWhenProjectHasLimits is the regression for issue #378:
// user-local maxWorkflowNesting / maxLoopIterations must fill unset project fields exactly like the
// byte limits do, even when the project already has a (partial) limits block. Previously the
// hand-maintained field list in mergeLimitsUnder omitted these two, so they were silently dropped
// whenever the project set any unrelated limit.
func TestApplyUserLocalUnder_nestingAndLoopFillWhenProjectHasLimits(t *testing.T) {
	project := &spec.ProjectSpec{
		Limits: &spec.ExecutionLimits{MaxToolInputBytes: 1024},
	}
	ApplyUserLocalUnder(project, &UserLocalOverlay{
		Limits: &spec.ExecutionLimits{
			MaxWorkflowNesting: 2,
			MaxLoopIterations:  5,
			MaxToolOutputBytes: 99,
		},
	})
	if project.Limits.MaxToolInputBytes != 1024 {
		t.Fatalf("project value should win, got MaxToolInputBytes=%d", project.Limits.MaxToolInputBytes)
	}
	if project.Limits.MaxToolOutputBytes != 99 {
		t.Fatalf("unset byte limit should fill from user-local, got %d", project.Limits.MaxToolOutputBytes)
	}
	if project.Limits.MaxWorkflowNesting != 2 {
		t.Fatalf("user-local maxWorkflowNesting dropped: got %d, want 2", project.Limits.MaxWorkflowNesting)
	}
	if project.Limits.MaxLoopIterations != 5 {
		t.Fatalf("user-local maxLoopIterations dropped: got %d, want 5", project.Limits.MaxLoopIterations)
	}

	// A project value for these fields still wins over the user-local overlay.
	project2 := &spec.ProjectSpec{
		Limits: &spec.ExecutionLimits{MaxWorkflowNesting: 4, MaxLoopIterations: 10},
	}
	ApplyUserLocalUnder(project2, &UserLocalOverlay{
		Limits: &spec.ExecutionLimits{MaxWorkflowNesting: 2, MaxLoopIterations: 5},
	})
	if project2.Limits.MaxWorkflowNesting != 4 || project2.Limits.MaxLoopIterations != 10 {
		t.Fatalf("project limits should win, got nesting=%d loop=%d", project2.Limits.MaxWorkflowNesting, project2.Limits.MaxLoopIterations)
	}
}
