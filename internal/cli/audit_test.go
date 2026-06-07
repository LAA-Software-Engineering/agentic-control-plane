package cli

import (
	"database/sql"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	_ "modernc.org/sqlite"
)

func TestAuditVerify_okAfterAppend(t *testing.T) {
	ctx := t.Context()
	db := filepath.Join(t.TempDir(), "audit-ok.db")
	root := runProjRoot(t)

	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: "run-audit", WorkflowName: "wf", Env: "dev", Status: "running",
		StartedAt: start, InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTraceEvent(ctx, "run-audit", start, "run_started", "agent", "", `{}`); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	ResetGlobalsForTest()
	rootCmd := NewRootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"audit", "verify", "--project", root, "--state", db, "--run", "run-audit"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestAuditVerify_failsOnTamper(t *testing.T) {
	ctx := t.Context()
	db := filepath.Join(t.TempDir(), "audit-bad.db")
	root := runProjRoot(t)

	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: "run-bad", WorkflowName: "wf", Env: "dev", Status: "running",
		StartedAt: start, InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTraceEvent(ctx, "run-bad", start, "run_started", "agent", "", `{}`); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	rawDB, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.ExecContext(ctx, `UPDATE trace_events SET data_json = '{"x":1}' WHERE run_id = 'run-bad'`); err != nil {
		t.Fatal(err)
	}
	_ = rawDB.Close()

	ResetGlobalsForTest()
	rootCmd := NewRootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"audit", "verify", "--project", root, "--state", db, "--run", "run-bad"})
	err = rootCmd.Execute()
	if err == nil {
		t.Fatal("expected verify failure")
	}
	if ExitCodeOf(err) != ExitGenericFailure {
		t.Fatalf("exit=%d want %d err=%v", ExitCodeOf(err), ExitGenericFailure, err)
	}
}

func TestAuditVerify_unknownRun(t *testing.T) {
	db := filepath.Join(t.TempDir(), "audit-none.db")
	root := runProjRoot(t)
	st, err := sqlite.Open(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	ResetGlobalsForTest()
	rootCmd := NewRootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"audit", "verify", "--project", root, "--state", db, "--run", "missing"})
	err = rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d want %d", ExitCodeOf(err), ExitValidationError)
	}
}
