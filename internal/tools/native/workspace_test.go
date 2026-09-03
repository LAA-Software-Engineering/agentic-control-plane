package native

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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

// TestWorkspace_SymlinkEscapeRejected is the boundary the sandbox exists to hold: a symlink
// *inside* the root pointing outside is lexically "within root" but escapes on access. os.Root
// resolves through openat and refuses it, in both directions — read and write.
func TestWorkspace_SymlinkEscapeRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privilege on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("TOP-SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envWorkspaceRoot, root)
	r := NewRegistry()

	if _, _, err := r.Dispatch(context.Background(), "read_file", map[string]any{"path": "escape/secret.txt"}); err == nil {
		t.Fatal("read_file through an in-sandbox symlink escaped the root")
	}
	if _, _, err := r.Dispatch(context.Background(), "write_file", map[string]any{"path": "escape/pwned.txt", "content": "OWNED"}); err == nil {
		t.Fatal("write_file through an in-sandbox symlink escaped the root")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned.txt")); err == nil {
		t.Fatal("write_file landed outside the sandbox via a symlink")
	}
}

// TestWorkspace_ReadFileTruncatesLargeFile confirms the read is bounded by the cap (not read
// whole then truncated): a file larger than the cap comes back truncated at exactly the cap.
func TestWorkspace_ReadFileTruncatesLargeFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	big := make([]byte, maxWorkspaceReadBytes+4096)
	for i := range big {
		big[i] = 'a'
	}
	if err := os.WriteFile(filepath.Join(root, "big.txt"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err := NewRegistry().Dispatch(context.Background(), "read_file", map[string]any{"path": "big.txt"})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if out["truncated"] != true {
		t.Fatalf("a file over the cap should be truncated: %#v", out)
	}
	if out["bytes"].(int) != maxWorkspaceReadBytes || len(out["content"].(string)) != maxWorkspaceReadBytes {
		t.Fatalf("truncated read should be exactly the cap, got bytes=%v len=%d", out["bytes"], len(out["content"].(string)))
	}
}

// TestWorkspace_ConfigRootBeatsEnv: a declared root on the context takes precedence over the env.
func TestWorkspace_ConfigRootBeatsEnv(t *testing.T) {
	envRoot := t.TempDir()
	cfgRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfgRoot, "only.txt"), []byte("from-config"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envWorkspaceRoot, envRoot)
	ctx := WithWorkspaceConfig(context.Background(), WorkspaceConfig{Root: cfgRoot})
	out, _, err := NewRegistry().Dispatch(ctx, "read_file", map[string]any{"path": "only.txt"})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if out["content"] != "from-config" {
		t.Fatalf("declared root should win over env, got %#v", out)
	}
}

// TestWorkspace_ConfigTestCommandBeatsEnv: a declared testCommand wins over the env var.
func TestWorkspace_ConfigTestCommandBeatsEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("run_tests uses sh -c")
	}
	root := t.TempDir()
	t.Setenv(envWorkspaceTestCommand, "exit 7")
	ctx := WithWorkspaceConfig(context.Background(), WorkspaceConfig{Root: root, TestCommand: "exit 0"})
	out, _, err := NewRegistry().Dispatch(ctx, "run_tests", map[string]any{})
	if err != nil {
		t.Fatalf("run_tests: %v", err)
	}
	if out["exitCode"].(int) != 0 {
		t.Fatalf("declared testCommand should win over env, got %#v", out)
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

func TestWorkspaceReadFile_directoryReturnsEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "framework", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "framework", "main.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := WithWorkspaceConfig(context.Background(), WorkspaceConfig{Root: dir})

	out, _, err := NewRegistry().Dispatch(ctx, "read_file", map[string]any{"path": "framework"})
	if err != nil {
		t.Fatalf("read_file on a directory should not error: %v", err)
	}
	if out["is_directory"] != true {
		t.Fatalf("is_directory = %v", out["is_directory"])
	}
	ents, ok := out["entries"].([]string)
	if !ok {
		t.Fatalf("entries type %T", out["entries"])
	}
	got := strings.Join(ents, ",")
	if got != "main.go,sub/" {
		t.Fatalf("entries = %q, want main.go,sub/", got)
	}
}

func TestWorkspaceReadFile_directoryTruncates(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "many")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxWorkspaceDirEntries+50; i++ {
		if err := os.WriteFile(filepath.Join(sub, "f"+strconv.Itoa(i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx := WithWorkspaceConfig(context.Background(), WorkspaceConfig{Root: dir})
	out, _, err := NewRegistry().Dispatch(ctx, "read_file", map[string]any{"path": "many"})
	if err != nil {
		t.Fatal(err)
	}
	if out["truncated"] != true {
		t.Fatalf("truncated = %v, want true", out["truncated"])
	}
	if ents := out["entries"].([]string); len(ents) != maxWorkspaceDirEntries {
		t.Fatalf("entries len = %d, want %d", len(ents), maxWorkspaceDirEntries)
	}
}
