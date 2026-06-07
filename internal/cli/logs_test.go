package cli

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

func extractRunID(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Run ID:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Run ID:"))
		}
	}
	return ""
}

func TestLogs_afterRun_showsStartedAndFinished(t *testing.T) {
	db := filepath.Join(t.TempDir(), "logs-run.db")
	root := runProjRoot(t)

	ResetGlobalsForTest()
	var runOut bytes.Buffer
	runCmd := NewRootCmd()
	runCmd.SetOut(&runOut)
	runCmd.SetErr(&runOut)
	runCmd.SetArgs([]string{
		"run", "workflow/demo",
		"--project", root,
		"-e", "staging",
		"--state", db,
		"--input", "topic=logs-test",
	})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	runID := extractRunID(runOut.String())
	if runID == "" {
		t.Fatalf("no run id in:\n%s", runOut.String())
	}

	ResetGlobalsForTest()
	var logOut bytes.Buffer
	logCmd := NewRootCmd()
	logCmd.SetOut(&logOut)
	logCmd.SetErr(&logOut)
	logCmd.SetArgs([]string{"logs", "--project", root, "--state", db, "--run", runID})
	if err := logCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := logOut.String()
	if !strings.Contains(s, string(trace.EventRunStarted)) {
		t.Fatalf("missing %s in:\n%s", trace.EventRunStarted, s)
	}
	if !strings.Contains(s, string(trace.EventRunFinished)) {
		t.Fatalf("missing %s in:\n%s", trace.EventRunFinished, s)
	}
}

func TestLogs_unknownRun_exit2(t *testing.T) {
	db := filepath.Join(t.TempDir(), "logs-none.db")
	root := runProjRoot(t)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"logs", "--project", root, "--state", db, "--run", "does-not-exist"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
}

func TestLogs_runAndWorkflowMutuallyExclusive(t *testing.T) {
	db := filepath.Join(t.TempDir(), "logs-both.db")
	root := runProjRoot(t)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"logs", "--project", root, "--state", db, "--run", "x", "--workflow", "y"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
}
