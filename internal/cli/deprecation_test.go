package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLI_yamlSourceDeprecationWarning is #440 Phase 2a: a command that loads a YAML project prints
// the deprecation warning to stderr, and a .agent-only project does not. It captures the real
// os.Stderr because the notice is emitted there (keeping `-o json` stdout clean). Not parallel — it
// swaps the process os.Stderr for the duration.
func TestCLI_yamlSourceDeprecationWarning(t *testing.T) {
	capture := func(root string) string {
		t.Helper()
		old := os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stderr = w
		ResetGlobalsForTest()
		cmd := NewRootCmd()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"validate", "--project", root})
		runErr := cmd.Execute()
		_ = w.Close()
		os.Stderr = old
		out, _ := io.ReadAll(r)
		if runErr != nil {
			t.Fatalf("validate --project %s: %v", root, runErr)
		}
		return string(out)
	}

	yamlRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(yamlRoot, "project.yaml"),
		[]byte("apiVersion: agentic.dev/v0\nkind: Project\nmetadata:\n  name: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := capture(yamlRoot); !strings.Contains(got, "YAML project authoring is deprecated") {
		t.Fatalf("YAML project must print the deprecation warning to stderr, got: %q", got)
	}

	agentRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(agentRoot, "main.agent"),
		[]byte("agent a {\n    model mock/default\n    instructions \"x\"\n}\n\nworkflow w(input: any) -> any {\n    return input\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := capture(agentRoot); strings.Contains(got, "deprecated") {
		t.Fatalf(".agent-only project must not print the deprecation warning, got: %q", got)
	}
}
