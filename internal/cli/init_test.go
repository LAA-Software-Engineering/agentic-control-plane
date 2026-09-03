package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Terfyn/terfyn/internal/config"
)

func TestInit_thenValidateSucceeds(t *testing.T) {
	parent := t.TempDir()
	name := "starter"

	ResetGlobalsForTest()
	icmd := NewRootCmd()
	icmd.SetOut(io.Discard)
	icmd.SetErr(io.Discard)
	icmd.SetArgs([]string{"init", name, "--parent-dir", parent})
	if err := icmd.Execute(); err != nil {
		t.Fatal(err)
	}

	proj := filepath.Join(parent, name)
	// Issue #430: init scaffolds a .agent-only project — main.agent is the sole source, and no
	// project.yaml is generated.
	if _, err := os.Stat(filepath.Join(proj, "main.agent")); err != nil {
		t.Fatalf("expected init to scaffold main.agent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, "project.yaml")); !os.IsNotExist(err) {
		t.Fatalf("init must NOT create project.yaml (issue #430), stat err = %v", err)
	}

	ResetGlobalsForTest()
	v := NewRootCmd()
	v.SetOut(io.Discard)
	v.SetErr(io.Discard)
	v.SetArgs([]string{"validate", "--project", proj})
	if err := v.Execute(); err != nil {
		t.Fatal(err)
	}
}

// TestInit_agentOnlyResolves confirms the generated .agent-only project resolves through
// config.Resolve (the path validate/plan/apply/run share) with no YAML — the agent + workflow are
// present and the config fingerprints (issue #430).
func TestInit_agentOnlyResolves(t *testing.T) {
	parent := t.TempDir()
	name := "hello"

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"init", name, "--parent-dir", parent})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	proj := filepath.Join(parent, name)
	rc, err := config.Resolve(config.ResolveOptions{ProjectRoot: proj})
	if err != nil {
		t.Fatalf("generated .agent-only project must resolve: %v", err)
	}
	g := rc.Graph()
	if g.Agents["assistant"] == nil || g.Workflows["hello"] == nil {
		t.Fatalf("generated project missing starter agent/workflow: agents=%d workflows=%d", len(g.Agents), len(g.Workflows))
	}
	if g.Meta.Name != name {
		t.Fatalf("project name = %q, want the directory basename %q", g.Meta.Name, name)
	}
}

func TestInit_rejectsExistingDir(t *testing.T) {
	parent := t.TempDir()
	name := "dup"
	if err := os.MkdirAll(filepath.Join(parent, name), 0o755); err != nil {
		t.Fatal(err)
	}

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"init", name, "--parent-dir", parent})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestInit_rejectsBadName(t *testing.T) {
	parent := t.TempDir()

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"init", "../nope", "--parent-dir", parent})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
}
