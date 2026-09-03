package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNew_deprecated is issue #430: `terfyn new` no longer scaffolds YAML resources — a Terfyn
// project is authored entirely in .agent source. Each resource subcommand returns a clear
// deprecation error (exit ExitValidationError) and writes nothing to disk.
func TestNew_deprecated(t *testing.T) {
	for _, kind := range []string{"tool", "policy", "workflow", "agent"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			ResetGlobalsForTest()
			cmd := NewRootCmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"new", kind, "foo", "--project", root})
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("new %s must be deprecated (error), got nil", kind)
			}
			if ExitCodeOf(err) != ExitValidationError {
				t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
			}
			if !strings.Contains(err.Error(), ".agent") || !strings.Contains(err.Error(), "deprecated") {
				t.Fatalf("want a .agent-authoring deprecation message, got %v", err)
			}
			// Nothing scaffolded: no YAML resource directories created.
			for _, dir := range []string{"tools", "policies", "workflows", "agents"} {
				if _, statErr := os.Stat(filepath.Join(root, dir)); !os.IsNotExist(statErr) {
					t.Fatalf("new must not scaffold %s/, stat err = %v", dir, statErr)
				}
			}
		})
	}
}
