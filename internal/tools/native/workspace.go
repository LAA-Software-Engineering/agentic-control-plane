package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Terfyn/terfyn/internal/tools/toolerr"
)

// Workspace adapter (issue #323): a sandboxed filesystem + test-runner native tool.
//
// The sandbox root and the test command are config that lives outside the agent. They may be
// declared on the Tool resource (spec.workspace.root / testCommand, issue #323 follow-up) or, when
// not declared, come from the environment:
//   - TERFYN_WORKSPACE_ROOT         — the sandbox root; every read/write path resolves within it.
//   - TERFYN_WORKSPACE_TEST_COMMAND — the run_tests command, run via `sh -c` in the root.
//
// Declared config (carried on the context by the tools registry, which resolves a relative root
// against the project root) takes precedence over the env fallback. run_tests takes its command
// from config, NEVER from tool-call arguments, so a granted agent cannot choose an arbitrary
// command to execute — the capability boundary holds.
const (
	envWorkspaceRoot        = "TERFYN_WORKSPACE_ROOT"
	envWorkspaceTestCommand = "TERFYN_WORKSPACE_TEST_COMMAND"

	// maxWorkspaceReadBytes caps a single read_file result; maxWorkspaceTestOutputBytes caps
	// captured run_tests output. Both guard against an unbounded read into a tool result.
	maxWorkspaceReadBytes       = 1 << 20  // 1 MiB
	maxWorkspaceTestOutputBytes = 64 << 10 // 64 KiB
	// maxWorkspaceDirEntries caps a read_file directory listing the same way (a big
	// node_modules or generated tree degrades to truncated=true rather than unbounded).
	maxWorkspaceDirEntries = 1000
)

// WorkspaceConfig is the declarative workspace config resolved from a Tool resource. Root is
// already absolute (the registry resolves a relative spec.workspace.root against the project root).
type WorkspaceConfig struct {
	Root        string
	TestCommand string
}

type workspaceConfigKey struct{}

// WithWorkspaceConfig carries a Tool's resolved workspace config on ctx so the native handlers use
// it in preference to the environment. The tools registry sets it per call when the Tool declares
// spec.workspace.
func WithWorkspaceConfig(ctx context.Context, cfg WorkspaceConfig) context.Context {
	return context.WithValue(ctx, workspaceConfigKey{}, cfg)
}

func workspaceConfigFromContext(ctx context.Context) WorkspaceConfig {
	if ctx == nil {
		return WorkspaceConfig{}
	}
	cfg, _ := ctx.Value(workspaceConfigKey{}).(WorkspaceConfig)
	return cfg
}

// workspaceRoot returns the absolute, existing sandbox root: the declared root on ctx if set,
// otherwise TERFYN_WORKSPACE_ROOT.
func workspaceRoot(ctx context.Context) (string, error) {
	root := strings.TrimSpace(workspaceConfigFromContext(ctx).Root)
	source := "spec.workspace.root"
	if root == "" {
		root = strings.TrimSpace(os.Getenv(envWorkspaceRoot))
		source = envWorkspaceRoot
	}
	if root == "" {
		return "", fmt.Errorf("native: no workspace root (set spec.workspace.root or %s)", envWorkspaceRoot)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("native: workspace root %q (%s): %w", root, source, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("native: workspace root %q (%s): %w", abs, source, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("native: workspace root %q (%s) is not a directory", abs, source)
	}
	return abs, nil
}

// workspaceTestCommand returns the run_tests command: the declared testCommand on ctx if set,
// otherwise TERFYN_WORKSPACE_TEST_COMMAND.
func workspaceTestCommand(ctx context.Context) (string, error) {
	cmd := strings.TrimSpace(workspaceConfigFromContext(ctx).TestCommand)
	if cmd == "" {
		cmd = strings.TrimSpace(os.Getenv(envWorkspaceTestCommand))
	}
	if cmd == "" {
		return "", fmt.Errorf("native: run_tests requires spec.workspace.testCommand or %s", envWorkspaceTestCommand)
	}
	return cmd, nil
}

// openWorkspaceRoot opens the sandbox root as an [os.Root]. Every read/write goes through the
// returned handle, whose methods resolve paths with openat semantics: a `..` component or a
// symlink that would leave the root is refused at the OS level (not by a string check), which
// also closes the check-then-open TOCTOU a lexical gate would leave. Callers must Close it.
func openWorkspaceRoot(ctx context.Context) (*os.Root, error) {
	dir, err := workspaceRoot(ctx)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("native: workspace root %q: %w", dir, err)
	}
	return r, nil
}

// cleanWorkspaceRel normalizes a tool-supplied path to a forward-slash, root-relative name for
// os.Root. A leading slash is treated as sandbox-root-relative (not an absolute escape); os.Root
// enforces the real boundary on the remaining components, so `..`/symlink escapes are refused there.
func cleanWorkspaceRel(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("field %q is required", "path")
	}
	rel = strings.TrimLeft(filepath.ToSlash(rel), "/")
	if rel == "" {
		return "", fmt.Errorf("field %q is required", "path")
	}
	return rel, nil
}

// readWorkspaceDirEntries reads a directory handle's entries as sorted names (sub-directories
// suffixed "/"), bounded at maxWorkspaceDirEntries so a pathological tree (a big node_modules, a
// generated tree) degrades to truncated=true rather than an unbounded read + oversized tool result.
// Shared by read_file's directory branch and list_dir so a cap change lands in one place.
func readWorkspaceDirEntries(f *os.File) (names []string, truncated bool, err error) {
	ents, rderr := f.ReadDir(maxWorkspaceDirEntries + 1)
	if rderr != nil && !errors.Is(rderr, io.EOF) {
		return nil, false, rderr
	}
	if len(ents) > maxWorkspaceDirEntries {
		ents = ents[:maxWorkspaceDirEntries]
		truncated = true
	}
	names = make([]string, 0, len(ents))
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, truncated, nil
}

// classifyWorkspacePathErr turns a filesystem error from a workspace path op into either a
// recoverable observation the agent can act on or a plain (fatal-by-default) error. Only a genuine
// MISS — the path does not exist (fs.ErrNotExist) — is recoverable, so `read_file` on a guessed
// path that isn't there lets the agent try another path instead of killing the run (issue #451). A
// sandbox-escape rejection from os.Root is NOT fs.ErrNotExist, and a permission error the agent
// cannot fix is not a miss, so both stay fatal. The observation echoes only rel — the agent's own
// input — never the underlying OS error text, which the fatal/trace path keeps for the operator.
func classifyWorkspacePathErr(op, rel string, err error) error {
	full := fmt.Errorf("native: %s %q: %w", op, rel, err)
	if errors.Is(err, fs.ErrNotExist) {
		return toolerr.Recoverable(fmt.Sprintf("%s: %q does not exist in the workspace", op, rel), full)
	}
	return full
}

func dispatchWorkspaceReadFile(ctx context.Context, with map[string]any, start time.Time) (map[string]any, ExecMeta, error) {
	meta := ExecMeta{DurationMs: time.Since(start).Milliseconds()}
	root, err := openWorkspaceRoot(ctx)
	if err != nil {
		return nil, meta, err
	}
	defer root.Close()
	rawPath, err := stringFromWith(with, "path")
	if err != nil {
		return nil, meta, fmt.Errorf("native: read_file: %w", err)
	}
	rel, err := cleanWorkspaceRel(rawPath)
	if err != nil {
		return nil, meta, fmt.Errorf("native: read_file: %w", err)
	}
	f, err := root.Open(rel)
	if err != nil {
		return nil, meta, classifyWorkspacePathErr("read_file", rel, err)
	}
	defer f.Close()
	// A directory is not an error: return its entries so an agent can explore the tree even via
	// read_file (list_dir is the dedicated op; this branch shares readWorkspaceDirEntries with it).
	// Sub-directories are marked with a trailing "/".
	if info, statErr := f.Stat(); statErr == nil && info.IsDir() {
		names, truncated, rderr := readWorkspaceDirEntries(f)
		if rderr != nil {
			return nil, meta, fmt.Errorf("native: read_file %q (directory): %w", rawPath, rderr)
		}
		meta.DurationMs = time.Since(start).Milliseconds()
		out := map[string]any{"path": rel, "is_directory": true, "entries": names}
		if truncated {
			out["truncated"] = true
		}
		return out, meta, nil
	}
	// Bound the read itself, not just the result: read at most one byte past the cap so a larger
	// file is reported truncated without loading all of it into memory.
	data, err := io.ReadAll(io.LimitReader(f, maxWorkspaceReadBytes+1))
	if err != nil {
		return nil, meta, fmt.Errorf("native: read_file %q: %w", rawPath, err)
	}
	truncated := false
	if len(data) > maxWorkspaceReadBytes {
		data = data[:maxWorkspaceReadBytes]
		truncated = true
	}
	out := map[string]any{
		"path":    rel,
		"content": string(data),
		"bytes":   len(data),
	}
	if truncated {
		out["truncated"] = true
	}
	meta.DurationMs = time.Since(start).Milliseconds()
	return out, meta, nil
}

func dispatchWorkspaceWriteFile(ctx context.Context, with map[string]any, start time.Time) (map[string]any, ExecMeta, error) {
	meta := ExecMeta{DurationMs: time.Since(start).Milliseconds()}
	root, err := openWorkspaceRoot(ctx)
	if err != nil {
		return nil, meta, err
	}
	defer root.Close()
	rawPath, err := stringFromWith(with, "path")
	if err != nil {
		return nil, meta, fmt.Errorf("native: write_file: %w", err)
	}
	content, err := contentFromWith(with)
	if err != nil {
		return nil, meta, fmt.Errorf("native: write_file: %w", err)
	}
	rel, err := cleanWorkspaceRel(rawPath)
	if err != nil {
		return nil, meta, fmt.Errorf("native: write_file: %w", err)
	}
	if dir := path.Dir(rel); dir != "." {
		// MkdirAll resolves through os.Root too, so a parent that escapes via `..`/symlink is refused.
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return nil, meta, fmt.Errorf("native: write_file %q: %w", rawPath, err)
		}
	}
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, meta, fmt.Errorf("native: write_file %q: %w", rawPath, err)
	}
	_, writeErr := f.Write([]byte(content))
	closeErr := f.Close()
	if writeErr != nil {
		return nil, meta, fmt.Errorf("native: write_file %q: %w", rawPath, writeErr)
	}
	if closeErr != nil {
		return nil, meta, fmt.Errorf("native: write_file %q: %w", rawPath, closeErr)
	}
	meta.DurationMs = time.Since(start).Milliseconds()
	return map[string]any{"path": rel, "bytes": len(content), "ok": true}, meta, nil
}

// contentFromWith reads the required string `content` arg. An explicitly empty string is a valid
// write (truncate to empty); only an absent content field is an error.
func contentFromWith(with map[string]any) (string, error) {
	v, ok := with["content"]
	if !ok || v == nil {
		return "", fmt.Errorf("field %q is required", "content")
	}
	s, err := scalarToString(v)
	if err != nil {
		return "", fmt.Errorf("field %q: %w", "content", err)
	}
	return s, nil
}

func dispatchWorkspaceRunTests(ctx context.Context, _ map[string]any, start time.Time) (map[string]any, ExecMeta, error) {
	meta := ExecMeta{DurationMs: time.Since(start).Milliseconds()}
	root, err := workspaceRoot(ctx)
	if err != nil {
		return nil, meta, err
	}
	command, err := workspaceTestCommand(ctx)
	if err != nil {
		return nil, meta, err
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = root
	combined, runErr := cmd.CombinedOutput()

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			// The command could not be started or was killed (e.g. context cancel/timeout):
			// that is a genuine tool failure, not a test result.
			return nil, meta, fmt.Errorf("native: run_tests %q: %w", command, runErr)
		}
	}

	output := string(combined)
	truncated := false
	if len(output) > maxWorkspaceTestOutputBytes {
		output = output[:maxWorkspaceTestOutputBytes]
		truncated = true
	}
	out := map[string]any{
		"command":  command,
		"exitCode": exitCode,
		"passed":   exitCode == 0,
		"output":   output,
	}
	if truncated {
		out["truncated"] = true
	}
	meta.DurationMs = time.Since(start).Milliseconds()
	return out, meta, nil
}
