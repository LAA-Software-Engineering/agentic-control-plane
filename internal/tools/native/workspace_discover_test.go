package native

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// seedWorkspace lays out a small tree under a fresh sandbox root and returns it. It spans three
// depths (root, one level, two levels) so recursive-glob and directory-pruning behavior is testable.
func seedWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"root.go":                  "package main\n\nfunc Root() {}\n",
		"framework/app.go":         "package framework\n\nfunc Route() {}\n",
		"framework/router_test.go": "package framework\n\nfunc TestRoute(t *testing.T) {}\n",
		"framework/xss_test.go":    "package framework\n\nfunc TestXSS(t *testing.T) {}\n",
		"pkg/internal/util.go":     "package internal\n\nfunc Util() {}\n",
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

func TestWorkspace_ListDir_notADirectoryErrors(t *testing.T) {
	t.Setenv(envWorkspaceRoot, seedWorkspace(t))
	_, _, err := NewRegistry().Dispatch(context.Background(), "list_dir", map[string]any{"path": "README.md"})
	if err == nil {
		t.Fatal("expected an error listing a file as a directory")
	}
}

// TestWorkspace_ListDir_mistypedPathErrors proves a present-but-non-scalar path is a bad-input error,
// not a silent fall-through to the workspace root (which would ignore the caller's scope).
func TestWorkspace_ListDir_mistypedPathErrors(t *testing.T) {
	t.Setenv(envWorkspaceRoot, seedWorkspace(t))
	_, _, err := NewRegistry().Dispatch(context.Background(), "list_dir", map[string]any{"path": []any{"framework"}})
	if err == nil {
		t.Fatal("list_dir must reject a non-string path, not list the root")
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

// TestWorkspace_Glob_recursive is the #452 contract: ** is a real globstar that crosses directory
// boundaries, so an agent's "**/*.go" enumerates the whole tree rather than a one-level subset (the
// io/fs.Glob / path.Match trap). A single * stays within one path segment.
func TestWorkspace_Glob_recursive(t *testing.T) {
	t.Setenv(envWorkspaceRoot, seedWorkspace(t))
	r := NewRegistry()

	// ** crosses directories: every .go file at any depth, including the two-level pkg/internal.
	out, _, err := r.Dispatch(context.Background(), "glob", map[string]any{"pattern": "**/*.go"})
	if err != nil {
		t.Fatalf("glob **/*.go: %v", err)
	}
	got := out["matches"].([]string)
	for _, want := range []string{"root.go", "framework/app.go", "pkg/internal/util.go", "cmd/main.go"} {
		if !containsStr(got, want) {
			t.Fatalf("glob **/*.go = %v, missing %q", got, want)
		}
	}

	// A single * does NOT cross '/': one-level *.go matches only the root file.
	out, _, err = r.Dispatch(context.Background(), "glob", map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatalf("glob *.go: %v", err)
	}
	got = out["matches"].([]string)
	if len(got) != 1 || got[0] != "root.go" {
		t.Fatalf("glob *.go = %v, want [root.go]", got)
	}

	// Scoped globstar: everything under pkg, however deep.
	out, _, err = r.Dispatch(context.Background(), "glob", map[string]any{"pattern": "pkg/**/*.go"})
	if err != nil {
		t.Fatalf("glob pkg/**/*.go: %v", err)
	}
	got = out["matches"].([]string)
	if len(got) != 1 || got[0] != "pkg/internal/util.go" {
		t.Fatalf("glob pkg/**/*.go = %v, want [pkg/internal/util.go]", got)
	}
}

func TestWorkspace_Glob_badPatternErrors(t *testing.T) {
	t.Setenv(envWorkspaceRoot, seedWorkspace(t))
	_, _, err := NewRegistry().Dispatch(context.Background(), "glob", map[string]any{"pattern": "[bad"})
	if err == nil {
		t.Fatal("expected a bad-pattern error")
	}
}

// TestWorkspace_Glob_pathEscapeRejected proves glob shares the sandbox boundary: a `..` pattern is
// rejected rather than silently returning an empty match set.
func TestWorkspace_Glob_pathEscapeRejected(t *testing.T) {
	t.Setenv(envWorkspaceRoot, seedWorkspace(t))
	if _, _, err := NewRegistry().Dispatch(context.Background(), "glob", map[string]any{"pattern": "../secret"}); err == nil {
		t.Fatal("glob must reject a .. escape pattern")
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

func TestWorkspace_Grep_badRegexpErrors(t *testing.T) {
	t.Setenv(envWorkspaceRoot, seedWorkspace(t))
	_, _, err := NewRegistry().Dispatch(context.Background(), "grep", map[string]any{"pattern": "("})
	if err == nil {
		t.Fatal("expected a bad-regexp error")
	}
}

// TestWorkspace_Grep_mistypedPathErrors proves a present-but-non-scalar scope is a bad-input error,
// not a silent fall-through that searches the whole workspace instead of the requested subtree.
func TestWorkspace_Grep_mistypedPathErrors(t *testing.T) {
	t.Setenv(envWorkspaceRoot, seedWorkspace(t))
	_, _, err := NewRegistry().Dispatch(context.Background(), "grep", map[string]any{"pattern": "Route", "path": map[string]any{"dir": "framework"}})
	if err == nil {
		t.Fatal("grep must reject a non-string path, not search the whole workspace")
	}
}

// TestWorkspace_Grep_cancelledContext proves the walk honors ctx: a cancelled run stops the search
// with an error rather than reading the whole tree.
func TestWorkspace_Grep_cancelledContext(t *testing.T) {
	t.Setenv(envWorkspaceRoot, seedWorkspace(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := NewRegistry().Dispatch(ctx, "grep", map[string]any{"pattern": "func"}); err == nil {
		t.Fatal("grep must fail on a cancelled context")
	}
}

// TestWorkspace_Grep_prunesDependencyDirs proves grep skips large vendored/dependency trees by
// default (so a match inside node_modules is not returned) but still searches them when the caller
// scopes directly at one.
func TestWorkspace_Grep_prunesDependencyDirs(t *testing.T) {
	root := seedWorkspace(t)
	dep := filepath.Join(root, "node_modules", "pkg")
	if err := os.MkdirAll(dep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dep, "index.js"), []byte("function Route() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envWorkspaceRoot, root)

	out, _, err := NewRegistry().Dispatch(context.Background(), "grep", map[string]any{"pattern": "Route"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	for _, m := range out["matches"].([]map[string]any) {
		if filepath.ToSlash(m["file"].(string)) == "node_modules/pkg/index.js" {
			t.Fatal("grep must prune node_modules by default")
		}
	}

	// Scoping directly at the pruned dir overrides the default and searches it.
	out, _, err = NewRegistry().Dispatch(context.Background(), "grep", map[string]any{"pattern": "Route", "path": "node_modules"})
	if err != nil {
		t.Fatalf("scoped grep: %v", err)
	}
	if out["count"].(int) == 0 {
		t.Fatal("grep scoped at node_modules must search it")
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
