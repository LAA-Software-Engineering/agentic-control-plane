package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLI_yamlSourceRejected is the ADR 007 successor to the Phase 2a deprecation warning: a command
// that finds a project.yaml manifest now fails with the migrate hint (YAML is no longer an accepted
// source), while a .agent-only project validates cleanly.
func TestCLI_yamlSourceRejected(t *testing.T) {
	validate := func(root string) error {
		t.Helper()
		ResetGlobalsForTest()
		cmd := NewRootCmd()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"validate", "--project", root})
		return cmd.Execute()
	}

	yamlRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(yamlRoot, "project.yaml"),
		[]byte("apiVersion: agentic.dev/v0\nkind: Project\nmetadata:\n  name: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := validate(yamlRoot)
	if err == nil {
		t.Fatal("a YAML project must be rejected")
	}
	if !strings.Contains(err.Error(), "no longer an accepted project source") || !strings.Contains(err.Error(), "migrate --to-agent") {
		t.Fatalf("want ADR 007 reject with migrate hint, got: %v", err)
	}

	agentRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(agentRoot, "main.agent"),
		[]byte("agent a {\n    model mock/default\n    instructions \"x\"\n}\n\nworkflow w(input: any) -> any {\n    return input\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validate(agentRoot); err != nil {
		t.Fatalf(".agent-only project must validate: %v", err)
	}
}
