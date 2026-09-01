package native

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestWorkspace_WriteReadRoundTrip exercises the registered ops through Dispatch: a write_file
// followed by read_file returns the same content, and the file lands inside the sandbox root.
func TestWorkspace_WriteReadRoundTrip(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	r := NewRegistry()

	out, _, err := r.Dispatch(context.Background(), "write_file", map[string]any{
		"path": "pkg/util.go", "content": "package util\n",
	})
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if out["ok"] != true || out["bytes"].(int) != len("package util\n") {
		t.Fatalf("write_file result: %#v", out)
	}
	if _, err := os.Stat(filepath.Join(root, "pkg", "util.go")); err != nil {
		t.Fatalf("file not created in sandbox: %v", err)
	}

	got, _, err := r.Dispatch(context.Background(), "read_file", map[string]any{"path": "pkg/util.go"})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if got["content"] != "package util\n" {
		t.Fatalf("read_file content = %q", got["content"])
	}
}

// TestWorkspace_ReadFileNoLongerUnknown pins the fix for issue #323: the operation dispatches
// (with a real error about the missing path) rather than returning ErrUnknownOperation.
func TestWorkspace_ReadFileNoLongerUnknown(t *testing.T) {
	t.Setenv(envWorkspaceRoot, t.TempDir())
	_, _, err := NewRegistry().Dispatch(context.Background(), "read_file", map[string]any{})
	if err == nil {
		t.Fatal("expected an error for read_file with no path")
	}
	if err.Error() == ErrUnknownOperation.Error() {
		t.Fatalf("read_file must be a known operation now, got %v", err)
	}
}

func TestWorkspace_PathEscapeRejected(t *testing.T) {
	root := t.TempDir()
	// A sibling file outside the sandbox that a traversal would target.
	outside := filepath.Join(filepath.Dir(root), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envWorkspaceRoot, root)
	r := NewRegistry()

	for _, p := range []string{"../secret.txt", "../../etc/passwd", "a/../../secret.txt"} {
		if _, _, err := r.Dispatch(context.Background(), "read_file", map[string]any{"path": p}); err == nil {
			t.Fatalf("path %q should be rejected as an escape", p)
		}
	}
}

// TestWorkspace_AbsolutePathContained confirms a leading slash does not escape: it is treated
// as sandbox-relative, so /util.go writes to <root>/util.go.
func TestWorkspace_AbsolutePathContained(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	if _, _, err := NewRegistry().Dispatch(context.Background(), "write_file", map[string]any{
		"path": "/util.go", "content": "x",
	}); err != nil {
		t.Fatalf("write_file /util.go: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "util.go")); err != nil {
		t.Fatalf("absolute path should be contained under root: %v", err)
	}
}

func TestWorkspace_RootRequired(t *testing.T) {
	t.Setenv(envWorkspaceRoot, "")
	if _, _, err := NewRegistry().Dispatch(context.Background(), "read_file", map[string]any{"path": "x"}); err == nil {
		t.Fatal("expected an error when the workspace root is unset")
	}
}

func TestWorkspace_RunTests(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("run_tests uses sh -c")
	}
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	r := NewRegistry()

	// Passing command: exit 0, output captured, run in the sandbox dir.
	t.Setenv(envWorkspaceTestCommand, "echo ok && pwd")
	out, _, err := r.Dispatch(context.Background(), "run_tests", map[string]any{})
	if err != nil {
		t.Fatalf("run_tests: %v", err)
	}
	if out["passed"] != true || out["exitCode"].(int) != 0 {
		t.Fatalf("expected passed run: %#v", out)
	}

	// Failing command: non-zero exit is a valid result, not a tool error.
	t.Setenv(envWorkspaceTestCommand, "exit 3")
	out, _, err = r.Dispatch(context.Background(), "run_tests", map[string]any{})
	if err != nil {
		t.Fatalf("a failing test command must not be a tool error: %v", err)
	}
	if out["passed"] != false || out["exitCode"].(int) != 3 {
		t.Fatalf("expected exitCode 3, not passed: %#v", out)
	}
}

func TestWorkspace_RunTestsCommandRequired(t *testing.T) {
	t.Setenv(envWorkspaceRoot, t.TempDir())
	t.Setenv(envWorkspaceTestCommand, "")
	if _, _, err := NewRegistry().Dispatch(context.Background(), "run_tests", map[string]any{}); err == nil {
		t.Fatalf("expected an error when %s is unset", envWorkspaceTestCommand)
	}
}
