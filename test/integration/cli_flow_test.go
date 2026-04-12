// End-to-end CLI tests (design doc §22, issue #32).
package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/cli"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

// repoRoot returns the agentic-control-plane module root (directory containing go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// test/integration/cli_flow_test.go -> repo root is ../..
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func runCLI(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()
	cli.ResetGlobalsForTest()
	cmd := cli.NewRootCmd()
	var b bytes.Buffer
	cmd.SetOut(&b)
	cmd.SetErr(&b)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return b.String(), err
}

func extractRunID(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Run ID:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Run ID:"))
		}
	}
	return ""
}

// TestCLI_ExampleMVPFlow exercises init → validate → plan → apply → run → logs in-process (§22, issue #32).
func TestCLI_ExampleMVPFlow(t *testing.T) {
	t.Run("init_validate_plan_apply_run_logs", func(t *testing.T) {
		parent := t.TempDir()
		projName := "e2eproj"
		projDir := filepath.Join(parent, projName)
		db := filepath.Join(t.TempDir(), "e2e-state.db")

		out, err := runCLI(t, "init", projName, "--parent-dir", parent)
		if err != nil {
			t.Fatalf("init: %v\n%s", err, out)
		}
		if _, err := os.Stat(filepath.Join(projDir, "project.yaml")); err != nil {
			t.Fatal(err)
		}

		out, err = runCLI(t, "validate", "--project", projDir, "--no-color")
		if err != nil {
			t.Fatalf("validate: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Validation successful") {
			t.Fatalf("validate output:\n%s", out)
		}

		out, err = runCLI(t, "plan", "--project", projDir, "--state", db)
		if err != nil {
			t.Fatalf("plan: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Plan: 4 to add, 0 to change, 0 to delete") {
			t.Fatalf("first plan:\n%s", out)
		}

		out, err = runCLI(t, "apply", "--project", projDir, "--state", db, "--auto-approve")
		if err != nil {
			t.Fatalf("apply: %v\n%s", err, out)
		}

		out, err = runCLI(t, "plan", "--project", projDir, "--state", db)
		if err != nil {
			t.Fatalf("second plan: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Plan: 0 to add, 0 to change, 0 to delete") {
			t.Fatalf("expected empty plan:\n%s", out)
		}

		out, err = runCLI(t, "run", "workflow/hello", "--project", projDir, "--state", db)
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Status: succeeded") {
			t.Fatalf("run output:\n%s", out)
		}
		runID := extractRunID(out)
		if runID == "" {
			t.Fatalf("no run id in:\n%s", out)
		}

		out, err = runCLI(t, "logs", "--project", projDir, "--state", db, "--run", runID)
		if err != nil {
			t.Fatalf("logs: %v\n%s", err, out)
		}
		if !strings.Contains(out, trace.EventRunStarted) || !strings.Contains(out, trace.EventRunFinished) {
			t.Fatalf("logs output:\n%s", out)
		}
	})

	t.Run("policy_denial_exit5", func(t *testing.T) {
		fixture := filepath.Join(repoRoot(t), "internal", "cli", "testdata", "run_policy")
		if _, err := os.Stat(filepath.Join(fixture, "project.yaml")); err != nil {
			t.Fatalf("fixture: %v", err)
		}
		db := filepath.Join(t.TempDir(), "policy-denial.db")

		_, err := runCLI(t,
			"run", "workflow/gated",
			"--project", fixture,
			"--state", db,
			"--input", "topic=x",
		)
		if err == nil {
			t.Fatal("expected policy denial error")
		}
		if cli.ExitCodeOf(err) != cli.ExitPolicyDenied {
			t.Fatalf("exit=%d want %d err=%v", cli.ExitCodeOf(err), cli.ExitPolicyDenied, err)
		}
	})
}
