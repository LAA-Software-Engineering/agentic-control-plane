package native

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// seedWorkspace lays out a small tree under a fresh sandbox root and returns it.
func seedWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"framework/app.go":         "package framework\n\nfunc Route() {}\n",
		"framework/router_test.go": "package framework\n\nfunc TestRoute(t *testing.T) {}\n",
		"framework/xss_test.go":    "package framework\n\nfunc TestXSS(t *testing.T) {}\n",
		"cmd/main.go":              "package main\n\nfunc main() { Route() }\n",
		"README.md":                "# demo\n",
	}
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestWorkspace_ListDir(t *testing.T) {
	t.Setenv(envWorkspaceRoot, seedWorkspace(t))
	r := NewRegistry()

	// Root listing (path omitted).
	out, _, err := r.Dispatch(context.Background(), "list_dir", map[string]any{})
	if err != nil {
		t.Fatalf("list_dir root: %v", err)
	}
	entries := out["entries"].([]string)
	if !containsStr(entries, "framework/") || !containsStr(entries, "README.md") || !containsStr(entries, "cmd/") {
		t.Fatalf("root entries = %v", entries)
	}

	// Subdirectory listing.
	out, _, err = r.Dispatch(context.Background(), "list_dir", map[string]any{"path": "framework"})
	if err != nil {
		t.Fatalf("list_dir framework: %v", err)
	}
	entries = out["entries"].([]string)
	for _, want := range []string{"app.go", "router_test.go", "xss_test.go"} {
		if !containsStr(entries, want) {
			t.Fatalf("framework entries = %v, missing %q", entries, want)
		}
	}
}

func TestWorkspace_ListDir_notADirectoryIsRecoverable(t *testing.T) {
	t.Setenv(envWorkspaceRoot, seedWorkspace(t))
	_, _, err := NewRegistry().Dispatch(context.Background(), "list_dir", map[string]any{"path": "README.md"})
	if err == nil {
		t.Fatal("expected an error listing a file as a directory")
	}
}

func TestWorkspace_Glob(t *testing.T) {
	t.Setenv(envWorkspaceRoot, seedWorkspace(t))
	out, _, err := NewRegistry().Dispatch(context.Background(), "glob", map[string]any{"pattern": "framework/*_test.go"})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	matches := out["matches"].([]string)
	want := []string{"framework/router_test.go", "framework/xss_test.go"}
	if len(matches) != 2 || matches[0] != want[0] || matches[1] != want[1] {
		t.Fatalf("glob matches = %v, want %v", matches, want)
	}
}

func TestWorkspace_Glob_badPatternIsRecoverable(t *testing.T) {
	t.Setenv(envWorkspaceRoot, seedWorkspace(t))
	_, _, err := NewRegistry().Dispatch(context.Background(), "glob", map[string]any{"pattern": "[bad"})
	if err == nil {
		t.Fatal("expected a bad-pattern error (recoverable)")
	}
}

func TestWorkspace_Grep(t *testing.T) {
	t.Setenv(envWorkspaceRoot, seedWorkspace(t))
	out, _, err := NewRegistry().Dispatch(context.Background(), "grep", map[string]any{"pattern": "func Route"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	matches := out["matches"].([]map[string]any)
	if len(matches) != 1 {
		t.Fatalf("grep matches = %v, want 1 hit", matches)
	}
	if matches[0]["file"] != "framework/app.go" || matches[0]["line"] != 3 {
		t.Fatalf("grep hit = %#v", matches[0])
	}

	// Scoped grep finds only under the given path.
	out, _, err = NewRegistry().Dispatch(context.Background(), "grep", map[string]any{"pattern": "func Test", "path": "framework"})
	if err != nil {
		t.Fatalf("scoped grep: %v", err)
	}
	if out["count"].(int) != 2 {
		t.Fatalf("scoped grep count = %v, want 2", out["count"])
	}
}

func TestWorkspace_Grep_badRegexpIsRecoverable(t *testing.T) {
	t.Setenv(envWorkspaceRoot, seedWorkspace(t))
	_, _, err := NewRegistry().Dispatch(context.Background(), "grep", map[string]any{"pattern": "("})
	if err == nil {
		t.Fatal("expected a bad-regexp error (recoverable)")
	}
}

func TestWorkspace_Grep_skipsBinary(t *testing.T) {
	root := seedWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, "blob.bin"), []byte("Route\x00\x00Route"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envWorkspaceRoot, root)
	out, _, err := NewRegistry().Dispatch(context.Background(), "grep", map[string]any{"pattern": "Route"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	for _, m := range out["matches"].([]map[string]any) {
		if m["file"] == "blob.bin" {
			t.Fatal("grep must skip a binary file")
		}
	}
}

// TestWorkspace_Discover_PathEscapeRejected proves the discovery ops share read_file's os.Root
// sandbox: a `..` escape cannot list or search outside the root.
func TestWorkspace_Discover_PathEscapeRejected(t *testing.T) {
	root := seedWorkspace(t)
	outside := filepath.Join(filepath.Dir(root), "secret")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "k.txt"), []byte("token"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envWorkspaceRoot, root)
	if _, _, err := NewRegistry().Dispatch(context.Background(), "list_dir", map[string]any{"path": "../secret"}); err == nil {
		t.Fatal("list_dir must reject a .. escape")
	}
	if _, _, err := NewRegistry().Dispatch(context.Background(), "grep", map[string]any{"pattern": "token", "path": "../secret"}); err == nil {
		t.Fatal("grep must reject a .. escape scope")
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
