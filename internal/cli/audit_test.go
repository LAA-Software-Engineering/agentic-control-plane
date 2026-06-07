package cli

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
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

func TestAuditVerify_JSON_contract(t *testing.T) {
	ctx := t.Context()
	db := filepath.Join(t.TempDir(), "audit-json.db")
	root := runProjRoot(t)

	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: "run-json", WorkflowName: "wf", Env: "dev", Status: "running",
		StartedAt: start, InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTraceEvent(ctx, "run-json", start, "run_started", "agent", "", `{}`); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	ResetGlobalsForTest()
	var buf bytes.Buffer
	rootCmd := NewRootCmd()
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"-o", "json", "audit", "verify", "--project", root, "--state", db, "--run", "run-json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	raw := bytes.TrimSpace(buf.Bytes())
	if !json.Valid(raw) {
		t.Fatalf("invalid json: %s", raw)
	}
	var payload struct {
		StatePath string `json:"statePath"`
		OK        bool   `json:"ok"`
		Runs      []struct {
			RunID     string `json:"runId"`
			OK        bool   `json:"ok"`
			Total     int    `json:"total"`
			Chained   int    `json:"chained"`
			Unchained int    `json:"unchained"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.StatePath == "" || len(payload.Runs) != 1 {
		t.Fatalf("payload=%+v", payload)
	}
	if payload.Runs[0].RunID != "run-json" || !payload.Runs[0].OK || payload.Runs[0].Chained != 1 {
		t.Fatalf("run=%+v", payload.Runs[0])
	}
}

func TestAuditVerify_defaultPath_listsRecentRuns(t *testing.T) {
	ctx := t.Context()
	db := filepath.Join(t.TempDir(), "audit-multi.db")
	root := runProjRoot(t)

	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	for _, id := range []string{"run-a", "run-b"} {
		if err := st.StartRun(ctx, state.Run{
			RunID: id, WorkflowName: "wf", Env: "dev", Status: "running",
			StartedAt: start, InputJSON: `{}`,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.AppendTraceEvent(ctx, id, start, "run_started", "agent", "", `{}`); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.Close()

	ResetGlobalsForTest()
	var buf bytes.Buffer
	rootCmd := NewRootCmd()
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"audit", "verify", "--project", root, "--state", db})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "run run-a: OK") || !strings.Contains(out, "run run-b: OK") {
		t.Fatalf("output:\n%s", out)
	}
}
