package native

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/tools/toolerr"
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

// TestWorkspace_ReadMissIsRecoverable proves a read_file on a path that does not exist is a
// RECOVERABLE tool error (issue #451): it carries the ErrRecoverable marker and a model-safe
// observation that names the agent's own path but not the underlying OS error text, so the agent
// can try another path instead of the run dying on the miss.
func TestWorkspace_ReadMissIsRecoverable(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	_, _, err := NewRegistry().Dispatch(context.Background(), "read_file", map[string]any{"path": "framework/nope.go"})
	if err == nil {
		t.Fatal("read_file on a missing path must error")
	}
	if !errors.Is(err, toolerr.ErrRecoverable) {
		t.Fatalf("a read miss must be recoverable, got %v", err)
	}
	obs, ok := toolerr.SafeObservation(err)
	if !ok {
		t.Fatalf("SafeObservation must report the miss recoverable: %v", err)
	}
	if !strings.Contains(obs, "framework/nope.go") {
		t.Fatalf("observation should name the agent's path: %q", obs)
	}
	if strings.Contains(obs, "no such file") {
		t.Fatalf("observation must not echo the raw OS error: %q", obs)
	}
}

// TestWorkspace_EscapeIsNotRecoverable proves a sandbox-escape rejection is FATAL, not a
// recoverable observation: it must not carry the ErrRecoverable marker (only a genuine miss does).
func TestWorkspace_EscapeIsNotRecoverable(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	_, _, err := NewRegistry().Dispatch(context.Background(), "read_file", map[string]any{"path": "../secret.txt"})
	if err == nil {
		t.Fatal("a .. escape must error")
	}
	if errors.Is(err, toolerr.ErrRecoverable) {
		t.Fatalf("a sandbox escape must stay fatal, not be recoverable: %v", err)
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

// --- read_file line range (offset/limit), issue #512 ---

func writeWorkspaceFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceReadFile_lineRange(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	writeWorkspaceFile(t, root, "f.txt", "one\ntwo\nthree\nfour\nfive\n")
	r := NewRegistry()

	// offset + limit returns exactly the requested span, with 1-based line bounds.
	out, _, err := r.Dispatch(context.Background(), "read_file", map[string]any{"path": "f.txt", "offset": 2, "limit": 2})
	if err != nil {
		t.Fatalf("read_file range: %v", err)
	}
	if out["content"] != "two\nthree\n" {
		t.Fatalf("content = %q, want two/three", out["content"])
	}
	if out["start_line"] != 2 || out["end_line"] != 3 || out["lines"] != 2 {
		t.Fatalf("bounds = start %v end %v lines %v", out["start_line"], out["end_line"], out["lines"])
	}

	// offset alone reads to EOF from that line.
	out, _, err = r.Dispatch(context.Background(), "read_file", map[string]any{"path": "f.txt", "offset": 4})
	if err != nil {
		t.Fatal(err)
	}
	if out["content"] != "four\nfive\n" || out["lines"] != 2 {
		t.Fatalf("offset-only = %q lines %v", out["content"], out["lines"])
	}

	// limit alone starts at line 1.
	out, _, err = r.Dispatch(context.Background(), "read_file", map[string]any{"path": "f.txt", "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	if out["content"] != "one\n" || out["start_line"] != 1 || out["end_line"] != 1 {
		t.Fatalf("limit-only = %q [%v,%v]", out["content"], out["start_line"], out["end_line"])
	}
}

func TestWorkspaceReadFile_wholeFileUnchangedWithoutRange(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	writeWorkspaceFile(t, root, "f.txt", "a\nb\n")
	out, _, err := NewRegistry().Dispatch(context.Background(), "read_file", map[string]any{"path": "f.txt"})
	if err != nil {
		t.Fatal(err)
	}
	// No range keys → today's whole-file shape (content + bytes, no start_line).
	if out["content"] != "a\nb\n" || out["bytes"] != 4 {
		t.Fatalf("whole-file = %#v", out)
	}
	if _, ok := out["start_line"]; ok {
		t.Fatalf("whole-file read should not carry line bounds: %#v", out)
	}
}

func TestWorkspaceReadFile_offsetPastEOF(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	writeWorkspaceFile(t, root, "f.txt", "a\nb\n")
	out, _, err := NewRegistry().Dispatch(context.Background(), "read_file", map[string]any{"path": "f.txt", "offset": 10})
	if err != nil {
		t.Fatal(err)
	}
	if out["content"] != "" || out["lines"] != 0 {
		t.Fatalf("past EOF should be empty, got %#v", out)
	}
	// end_line < start_line signals no lines matched.
	if out["start_line"] != 10 || out["end_line"] != 9 {
		t.Fatalf("bounds = [%v,%v]", out["start_line"], out["end_line"])
	}
}

func TestWorkspaceReadFile_rangeReachesPastByteCap(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	// A file larger than the whole-file byte cap; the target line lives past the cap. A range read
	// must still reach it (it reads line by line, not a single capped slurp).
	var b strings.Builder
	deep := maxWorkspaceReadBytes/2 + 1000 // line number well past 1 MiB of "x\n"
	for i := 1; i <= deep; i++ {
		if i == deep {
			b.WriteString("NEEDLE\n")
		} else {
			b.WriteString("x\n")
		}
	}
	writeWorkspaceFile(t, root, "big.txt", b.String())
	out, _, err := NewRegistry().Dispatch(context.Background(), "read_file", map[string]any{"path": "big.txt", "offset": deep, "limit": 1})
	if err != nil {
		t.Fatalf("deep range: %v", err)
	}
	if out["content"] != "NEEDLE\n" {
		t.Fatalf("deep range content = %q, want NEEDLE", out["content"])
	}
}

func TestWorkspaceReadFile_badRangeArgs(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	writeWorkspaceFile(t, root, "f.txt", "a\nb\n")
	r := NewRegistry()
	for _, with := range []map[string]any{
		{"path": "f.txt", "offset": 0},
		{"path": "f.txt", "limit": 0},
		{"path": "f.txt", "offset": -1},
		{"path": "f.txt", "offset": "notanint"},
		{"path": "f.txt", "limit": 1.5},
	} {
		if _, _, err := r.Dispatch(context.Background(), "read_file", with); err == nil {
			t.Fatalf("expected bad-input error for %v", with)
		}
	}
}

// --- edit (str_replace), issue #512 ---

func TestWorkspaceEdit_replacesUniqueOccurrence(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	writeWorkspaceFile(t, root, "pkg/x.go", "package x\n\nfunc A() int { return 1 }\n")
	r := NewRegistry()

	out, _, err := r.Dispatch(context.Background(), "edit", map[string]any{
		"path": "pkg/x.go", "old_string": "return 1", "new_string": "return 2",
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if out["ok"] != true || out["replaced"] != 1 {
		t.Fatalf("edit result: %#v", out)
	}
	got, err := os.ReadFile(filepath.Join(root, "pkg", "x.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package x\n\nfunc A() int { return 2 }\n" {
		t.Fatalf("file after edit = %q", got)
	}
}

func TestWorkspaceEdit_deletionWithEmptyNewString(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	writeWorkspaceFile(t, root, "f.txt", "keep REMOVE keep\n")
	out, _, err := NewRegistry().Dispatch(context.Background(), "edit", map[string]any{
		"path": "f.txt", "old_string": " REMOVE", "new_string": "",
	})
	if err != nil {
		t.Fatalf("edit deletion: %v", err)
	}
	if out["ok"] != true {
		t.Fatalf("edit result: %#v", out)
	}
	got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(got) != "keep keep\n" {
		t.Fatalf("after deletion = %q", got)
	}
}

func TestWorkspaceEdit_notFoundIsRecoverable(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	writeWorkspaceFile(t, root, "f.txt", "hello world\n")
	_, _, err := NewRegistry().Dispatch(context.Background(), "edit", map[string]any{
		"path": "f.txt", "old_string": "absent", "new_string": "x",
	})
	if err == nil {
		t.Fatal("expected error for missing old_string")
	}
	if !errors.Is(err, toolerr.ErrRecoverable) {
		t.Fatalf("old_string-not-found should be recoverable, got %v", err)
	}
	// The file is unchanged.
	got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(got) != "hello world\n" {
		t.Fatalf("file must be untouched on a failed edit, got %q", got)
	}
}

func TestWorkspaceEdit_ambiguousMatchIsRecoverable(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	writeWorkspaceFile(t, root, "f.txt", "x = 1\nx = 1\n")
	_, _, err := NewRegistry().Dispatch(context.Background(), "edit", map[string]any{
		"path": "f.txt", "old_string": "x = 1", "new_string": "x = 2",
	})
	if err == nil {
		t.Fatal("expected error for non-unique old_string")
	}
	if !errors.Is(err, toolerr.ErrRecoverable) {
		t.Fatalf("non-unique old_string should be recoverable, got %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(got) != "x = 1\nx = 1\n" {
		t.Fatalf("file must be untouched on an ambiguous edit, got %q", got)
	}
}

func TestWorkspaceEdit_rejectsEmptyOldOrIdentical(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	writeWorkspaceFile(t, root, "f.txt", "abc\n")
	r := NewRegistry()
	if _, _, err := r.Dispatch(context.Background(), "edit", map[string]any{"path": "f.txt", "old_string": "", "new_string": "x"}); err == nil {
		t.Fatal("empty old_string should be rejected")
	}
	if _, _, err := r.Dispatch(context.Background(), "edit", map[string]any{"path": "f.txt", "old_string": "abc", "new_string": "abc"}); err == nil {
		t.Fatal("identical old/new should be rejected")
	}
}

func TestWorkspaceEdit_missingFileIsRecoverable(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	_, _, err := NewRegistry().Dispatch(context.Background(), "edit", map[string]any{
		"path": "nope.txt", "old_string": "a", "new_string": "b",
	})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, toolerr.ErrRecoverable) {
		t.Fatalf("edit on a missing path should be recoverable (a miss), got %v", err)
	}
}

func TestWorkspaceEdit_pathEscapeRejected(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envWorkspaceRoot, root)
	_, _, err := NewRegistry().Dispatch(context.Background(), "edit", map[string]any{
		"path": "../secret.txt", "old_string": "secret", "new_string": "leaked",
	})
	if err == nil {
		t.Fatal("edit through a traversal must be refused")
	}
	if errors.Is(err, toolerr.ErrRecoverable) {
		t.Fatalf("a sandbox escape is not a recoverable miss: %v", err)
	}
	got, _ := os.ReadFile(outside)
	if string(got) != "secret" {
		t.Fatalf("outside file must be untouched, got %q", got)
	}
}

// A range read over a giant single line (larger than both the scan buffer and the 1 MiB result cap)
// must bound the RETURNED content at the cap and flag truncation, not load the whole line (issue #512
// review). This is the case the "x\n"-lines deep-range test cannot reach.
func TestWorkspaceReadFile_giantLineIsBounded(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	giant := strings.Repeat("a", maxWorkspaceReadBytes*2) // one line, no newline, 2x the cap
	writeWorkspaceFile(t, root, "min.js", giant)
	out, _, err := NewRegistry().Dispatch(context.Background(), "read_file", map[string]any{"path": "min.js", "offset": 1, "limit": 1})
	if err != nil {
		t.Fatalf("range read: %v", err)
	}
	if out["truncated"] != true {
		t.Fatalf("a giant line must be truncated: %#v keys", out["truncated"])
	}
	if got := len(out["content"].(string)); got != maxWorkspaceReadBytes {
		t.Fatalf("returned content = %d bytes, want the cap %d", got, maxWorkspaceReadBytes)
	}
	// The span fields must describe the bytes returned: a truncated PREFIX of line 1 is still line 1.
	if out["lines"] != 1 || out["start_line"] != 1 || out["end_line"] != 1 {
		t.Fatalf("span for a truncated line 1 = lines %v [%v,%v], want lines 1 [1,1]", out["lines"], out["start_line"], out["end_line"])
	}
}

// Complete lines that fill the byte cap exactly, then one more short line: the short line contributes
// no bytes (room<=0), so it must NOT be counted — otherwise offset=end_line+1 pagination would skip it
// (issue #512 review).
func TestWorkspaceReadFile_capFilledExactlyDoesNotCountEmptyLine(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	nLines := maxWorkspaceReadBytes / 2 // "x\n" is 2 bytes, so nLines lines == the cap exactly
	var sb strings.Builder
	for i := 0; i < nLines; i++ {
		sb.WriteString("x\n")
	}
	sb.WriteString("y\n") // the line that must not be counted
	writeWorkspaceFile(t, root, "f.txt", sb.String())

	out, _, err := NewRegistry().Dispatch(context.Background(), "read_file", map[string]any{"path": "f.txt", "offset": 1})
	if err != nil {
		t.Fatalf("range read: %v", err)
	}
	if out["truncated"] != true {
		t.Fatalf("filling the cap should truncate: %#v", out["truncated"])
	}
	if out["lines"] != nLines || out["end_line"] != nLines {
		t.Fatalf("span = lines %v end_line %v, want %d/%d (the y line must not count)", out["lines"], out["end_line"], nLines, nLines)
	}
	if strings.Contains(out["content"].(string), "y") {
		t.Fatalf("content should not include the uncounted line")
	}
	// Paging from end_line+1 must resume at the dropped short line, not past it.
	next, _, err := NewRegistry().Dispatch(context.Background(), "read_file", map[string]any{"path": "f.txt", "offset": out["end_line"].(int) + 1, "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	if next["content"] != "y\n" {
		t.Fatalf("pagination resumed at %q, want the dropped y line", next["content"])
	}
}

// A cancelled context stops the range scan instead of reading to EOF (issue #512 review).
func TestWorkspaceReadFile_rangeHonorsContext(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	writeWorkspaceFile(t, root, "f.txt", "a\nb\nc\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := NewRegistry().Dispatch(ctx, "read_file", map[string]any{"path": "f.txt", "offset": 1, "limit": 2})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled range read err = %v, want context.Canceled", err)
	}
}

// A failed edit write must not destroy the original file (issue #512 review): edit writes a temp then
// renames, so if the write cannot happen (here the parent directory is not writable) the original is
// left exactly as it was — unlike an in-place O_TRUNC, which would have zeroed it first.
func TestWorkspaceEdit_failedWriteLeavesOriginalIntact(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix directory permissions")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	const original = "func A() int { return 1 }\n"
	writeWorkspaceFile(t, root, "sub/x.go", original)
	// Make the containing directory non-writable so creating the sibling temp fails.
	dir := filepath.Join(root, "sub")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, _, err := NewRegistry().Dispatch(context.Background(), "edit", map[string]any{
		"path": "sub/x.go", "old_string": "return 1", "new_string": "return 2",
	})
	if err == nil {
		t.Fatal("expected the edit to fail on a non-writable directory")
	}
	// Restore write access to read the file back and prove it is byte-for-byte the original.
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, rerr := os.ReadFile(filepath.Join(dir, "x.go"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(got) != original {
		t.Fatalf("original must survive a failed edit, got %q", got)
	}
}

// A successful edit leaves no temp file behind and preserves the original file's mode (issue #512).
func TestWorkspaceEdit_atomicNoLeftoverAndPreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file mode")
	}
	root := t.TempDir()
	t.Setenv(envWorkspaceRoot, root)
	writeWorkspaceFile(t, root, "run.sh", "echo one\n")
	target := filepath.Join(root, "run.sh")
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewRegistry().Dispatch(context.Background(), "edit", map[string]any{
		"path": "run.sh", "old_string": "one", "new_string": "two",
	}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	// The executable bit survives the rewrite.
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755 preserved", info.Mode().Perm())
	}
	// No sibling temp files linger in the workspace root.
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.Contains(e.Name(), "terfyn-edit") || strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}
