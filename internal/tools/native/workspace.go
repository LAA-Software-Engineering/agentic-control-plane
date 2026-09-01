package native

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Workspace adapter (issue #323): a sandboxed filesystem + test-runner native tool.
//
// The sandbox root and the test command come from the environment, matching the github
// adapter's env-based config (GITHUB_TOKEN / GITHUB_API_URL). Config lives outside the agent:
//   - TERFYN_WORKSPACE_ROOT       — the sandbox root; every read/write path resolves within it.
//   - TERFYN_WORKSPACE_TEST_COMMAND — the run_tests command, run via `sh -c` in the root.
//
// run_tests takes its command from config, NEVER from tool-call arguments, so a granted agent
// cannot choose an arbitrary command to execute — the capability boundary holds.
const (
	envWorkspaceRoot        = "TERFYN_WORKSPACE_ROOT"
	envWorkspaceTestCommand = "TERFYN_WORKSPACE_TEST_COMMAND"

	// maxWorkspaceReadBytes caps a single read_file result; maxWorkspaceTestOutputBytes caps
	// captured run_tests output. Both guard against an unbounded read into a tool result.
	maxWorkspaceReadBytes       = 1 << 20  // 1 MiB
	maxWorkspaceTestOutputBytes = 64 << 10 // 64 KiB
)

// workspaceRoot returns the absolute, existing sandbox root or an error naming the env var.
func workspaceRoot() (string, error) {
	root := strings.TrimSpace(os.Getenv(envWorkspaceRoot))
	if root == "" {
		return "", fmt.Errorf("native: %s is not set (required for workspace operations)", envWorkspaceRoot)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("native: workspace root %q: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("native: workspace root %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("native: workspace root %q is not a directory", abs)
	}
	return abs, nil
}

// resolveWorkspacePath joins rel onto the sandbox root and confirms the result stays within it.
// A leading slash is not honored as an absolute escape (filepath.Join contains it); `..` traversal
// that would leave the root is rejected.
func resolveWorkspacePath(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("field %q is required", "path")
	}
	full := filepath.Join(root, filepath.Clean("/"+rel)) // leading "/" neutralizes absolute paths
	within, err := filepath.Rel(root, full)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes the workspace root", rel)
	}
	return full, nil
}

func dispatchWorkspaceReadFile(_ context.Context, with map[string]any, start time.Time) (map[string]any, ExecMeta, error) {
	meta := ExecMeta{DurationMs: time.Since(start).Milliseconds()}
	root, err := workspaceRoot()
	if err != nil {
		return nil, meta, err
	}
	rel, err := stringFromWith(with, "path")
	if err != nil {
		return nil, meta, fmt.Errorf("native: read_file: %w", err)
	}
	full, err := resolveWorkspacePath(root, rel)
	if err != nil {
		return nil, meta, fmt.Errorf("native: read_file: %w", err)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, meta, fmt.Errorf("native: read_file %q: %w", rel, err)
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

func dispatchWorkspaceWriteFile(_ context.Context, with map[string]any, start time.Time) (map[string]any, ExecMeta, error) {
	meta := ExecMeta{DurationMs: time.Since(start).Milliseconds()}
	root, err := workspaceRoot()
	if err != nil {
		return nil, meta, err
	}
	rel, err := stringFromWith(with, "path")
	if err != nil {
		return nil, meta, fmt.Errorf("native: write_file: %w", err)
	}
	content, err := contentFromWith(with)
	if err != nil {
		return nil, meta, fmt.Errorf("native: write_file: %w", err)
	}
	full, err := resolveWorkspacePath(root, rel)
	if err != nil {
		return nil, meta, fmt.Errorf("native: write_file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, meta, fmt.Errorf("native: write_file %q: %w", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return nil, meta, fmt.Errorf("native: write_file %q: %w", rel, err)
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
	root, err := workspaceRoot()
	if err != nil {
		return nil, meta, err
	}
	command := strings.TrimSpace(os.Getenv(envWorkspaceTestCommand))
	if command == "" {
		return nil, meta, fmt.Errorf("native: run_tests requires %s to be set", envWorkspaceTestCommand)
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
