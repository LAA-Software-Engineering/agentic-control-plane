package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/inspect"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
)

func TestInspect_web_flagsAndAPI(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "web.db")
	root := runProjRoot(t)

	// Create state via a workflow run.
	ResetGlobalsForTest()
	runCmd := NewRootCmd()
	runCmd.SetOut(io.Discard)
	runCmd.SetErr(io.Discard)
	runCmd.SetArgs([]string{
		"run", "workflow/demo",
		"--project", root,
		"-e", "staging",
		"--state", db,
		"--input", "topic=inspect-web",
	})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	st, err := sqlite.OpenReadOnly(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	runs, err := st.ListRecentRuns(ctx, 5)
	if err != nil || len(runs) == 0 {
		t.Fatalf("runs=%v err=%v", runs, err)
	}

	srv, err := inspect.NewServer(st, inspect.Config{
		StatePath: db,
		Env:       "staging",
		Port:      0, // not used when served via httptest pattern below
	})
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	res, err := http.Get(ts.URL + "/api/runs")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	b, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(b), runs[0].RunID) {
		t.Fatalf("body missing run id: %s", b)
	}
}

func TestInspect_web_requiresNoArgs(t *testing.T) {
	db := filepath.Join(t.TempDir(), "missing.db")
	root := runProjRoot(t)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"inspect", "--web", "--project", root, "--state", db, "Workflow/demo"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
}

func TestInspect_web_invalidTraceUI_exit2(t *testing.T) {
	root := runProjRoot(t)
	db := filepath.Join(t.TempDir(), "ui.db")

	ResetGlobalsForTest()
	runCmd := NewRootCmd()
	runCmd.SetOut(io.Discard)
	runCmd.SetErr(io.Discard)
	runCmd.SetArgs([]string{"run", "workflow/demo", "--project", root, "--state", db, "--input", "topic=trace-ui-test"})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"inspect", "--web", "--project", root, "--state", db, "--trace-ui", "javascript:alert(1)"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
}

func TestInspect_web_missingDB_exit2(t *testing.T) {
	db := filepath.Join(t.TempDir(), "no.db")
	root := runProjRoot(t)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"inspect", "--web", "--project", root, "--state", db})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
}

func TestInspect_web_readOnlyOpen(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "ro-cli.db")
	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.StartRun(ctx, state.Run{
		RunID: "r", WorkflowName: "w", Env: "local", Status: state.RunStatusRunning,
		StartedAt: time.Now().UTC(), InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	ro, err := sqlite.OpenReadOnly(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	_ = ro.Close()
}
