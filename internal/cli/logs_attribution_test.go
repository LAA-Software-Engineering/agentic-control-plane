package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/statejson"
)

func TestLogs_filterByTenantAndThread(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "logs-filter.db")
	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	start := time.Date(2026, 6, 4, 10, 0, 0, 0, time.UTC)
	for _, r := range []state.Run{
		{RunID: "match", WorkflowName: "wf", Env: "local", Status: "succeeded", StartedAt: start,
			InputJSON: `{}`, TenantID: "acme", ThreadID: "session-1", ActorID: "u1", RequestID: "r1", Source: "cli"},
		{RunID: "other-thread", WorkflowName: "wf", Env: "local", Status: "succeeded", StartedAt: start.Add(time.Minute),
			InputJSON: `{}`, TenantID: "acme", ThreadID: "session-2", ActorID: "u1", RequestID: "r2", Source: "cli"},
	} {
		if err := st.StartRun(ctx, r); err != nil {
			t.Fatal(err)
		}
		if _, err := st.AppendTraceEvent(ctx, r.RunID, start, "run_started", "agent", "", `{}`); err != nil {
			t.Fatal(err)
		}
	}

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	root := runProjRoot(t)
	cmd.SetArgs([]string{"logs", "--tenant-id", "acme", "--thread-id", "session-1", "--project", root, "--state", db, "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		Runs []struct {
			RunID  string                       `json:"runId"`
			Events []statejson.TraceEventRecord `json:"events"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if len(payload.Runs) != 1 || payload.Runs[0].RunID != "match" {
		t.Fatalf("runs: %+v", payload.Runs)
	}
	if len(payload.Runs[0].Events) != 1 || payload.Runs[0].Events[0].TenantID != "acme" || payload.Runs[0].Events[0].ThreadID != "session-1" {
		t.Fatalf("events: %+v", payload.Runs[0].Events)
	}
}

func TestLogs_runListShowsAttributionColumns(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "logs-table.db")
	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	start := time.Now().UTC()
	if err := st.StartRun(ctx, state.Run{
		RunID: "r1", WorkflowName: "wf", Env: "local", Status: "running", StartedAt: start,
		InputJSON: `{}`, TenantID: "t", ThreadID: "th", ActorID: "a", RequestID: "req", Source: "cli",
	}); err != nil {
		t.Fatal(err)
	}

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	root := runProjRoot(t)
	cmd.SetArgs([]string{"logs", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "TENANT") || !strings.Contains(text, "THREAD") || !strings.Contains(text, "ACTOR") {
		t.Fatalf("table headers:\n%s", text)
	}
	if !strings.Contains(text, "r1") || !strings.Contains(text, " t ") || !strings.Contains(text, " th ") || !strings.Contains(text, " a ") {
		t.Fatalf("table values:\n%s", text)
	}
}

func TestLogs_filterByActorOnly(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "logs-actor.db")
	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	start := time.Now().UTC()
	for _, r := range []state.Run{
		{RunID: "a", WorkflowName: "wf", Env: "local", Status: "succeeded", StartedAt: start,
			InputJSON: `{}`, TenantID: "t1", ThreadID: "th1", ActorID: "target", RequestID: "r1", Source: "cli"},
		{RunID: "b", WorkflowName: "wf", Env: "local", Status: "succeeded", StartedAt: start,
			InputJSON: `{}`, TenantID: "t1", ThreadID: "th1", ActorID: "other", RequestID: "r2", Source: "cli"},
	} {
		if err := st.StartRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	root := runProjRoot(t)
	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"logs", "--actor-id", "target", "--project", root, "--state", db, "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Runs []struct {
			RunID string `json:"runId"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Runs) != 1 || payload.Runs[0].RunID != "a" {
		t.Fatalf("runs: %+v", payload.Runs)
	}
}

func TestLogs_rejectsRunWithTenantFilter(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"logs", "--run", "r1", "--tenant-id", "acme"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("err = %v", err)
	}
}
