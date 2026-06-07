package cli

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

func TestLogs_eventFilter(t *testing.T) {
	db := filepath.Join(t.TempDir(), "logs-event.db")
	root := runProjRoot(t)

	ResetGlobalsForTest()
	var runOut bytes.Buffer
	runCmd := NewRootCmd()
	runCmd.SetOut(&runOut)
	runCmd.SetErr(&runOut)
	runCmd.SetArgs([]string{
		"run", "workflow/demo",
		"--project", root,
		"--state", db,
		"--input", "topic=event-filter",
	})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	runID := extractRunID(runOut.String())
	if runID == "" {
		t.Fatal("no run id")
	}

	ResetGlobalsForTest()
	var filtered bytes.Buffer
	filterCmd := NewRootCmd()
	filterCmd.SetOut(&filtered)
	filterCmd.SetErr(&filtered)
	filterCmd.SetArgs([]string{
		"logs", "--project", root, "--state", db, "--run", runID,
		"--event", string(trace.EventToolExecution),
	})
	if err := filterCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := filtered.String()
	if !strings.Contains(out, string(trace.EventToolExecution)) {
		t.Fatalf("missing filtered event in:\n%s", out)
	}
	if strings.Contains(out, string(trace.EventRunStarted)) {
		t.Fatalf("run_started should be filtered out:\n%s", out)
	}
}

func TestLogs_unknownEvent_exit2(t *testing.T) {
	db := filepath.Join(t.TempDir(), "logs-bad-event.db")
	root := runProjRoot(t)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"logs", "--project", root, "--state", db, "--run", "any",
		"--event", "not_a_real_event",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
	if !strings.Contains(err.Error(), "tool_execution") {
		t.Fatalf("expected known types in error: %v", err)
	}
}

func TestLogs_eventFilter_listsKnownTypesInError(t *testing.T) {
	_, err := parseLogsEventFilter([]string{"bogus"})
	if err == nil {
		t.Fatal("expected error")
	}
	for _, known := range trace.AllEventTypeStrings() {
		if !strings.Contains(err.Error(), known) {
			t.Fatalf("error should list %q: %v", known, err)
		}
	}
}
