package mcp

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func mockMCPExecutable(t *testing.T) string {
	t.Helper()
	name := "mockmcp"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", out, "./testdata/mockmcp")
	cmd.Dir = "."
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build mockmcp: %v\n%s", err, b)
	}
	return out
}

func TestCallStdio_mockSubprocess(t *testing.T) {
	bin := mockMCPExecutable(t)
	ctx := context.Background()
	out, meta, err := CallStdio(ctx, &spec.ToolMCP{
		Transport: "stdio",
		Command:   bin,
	}, nil, "any", map[string]any{"repo": "acme/api", "n": float64(3)})
	if err != nil {
		t.Fatal(err)
	}
	if meta.DurationMs < 0 {
		t.Fatalf("meta %+v", meta)
	}
	if out["repo"] != "acme/api" || out["n"] != float64(3) {
		t.Fatalf("output %+v", out)
	}
}

func TestCallStdio_retryOnTransportFailure(t *testing.T) {
	ctx := context.Background()
	_, _, err := CallStdio(ctx, &spec.ToolMCP{
		Transport: "stdio",
		Command:   "/nonexistent/binary/mcp-missing-xyz",
	}, &spec.ToolRetry{MaxAttempts: 2, Backoff: "fixed"}, "x", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
