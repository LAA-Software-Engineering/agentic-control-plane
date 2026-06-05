package cli

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
)

func TestRun_persistsAttributionFlags(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "run-attr.db")
	root := runProjRoot(t)

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"run", "workflow/demo",
		"--project", root,
		"--state", db,
		"--input", "topic=attr-test",
		"--tenant-id", "acme",
		"--thread-id", "ci-thread-9",
		"--actor-id", "ci-bot",
		"--source", "actions",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	runID := extractRunID(out.String())
	if runID == "" {
		t.Fatal("no run id in output")
	}

	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "acme" || got.ThreadID != "ci-thread-9" || got.ActorID != "ci-bot" || got.Source != "actions" {
		t.Fatalf("attribution: %+v", got)
	}
	if got.RequestID == "" {
		t.Fatal("expected request_id")
	}

	events, err := st.ListTraceEventsByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("no trace events")
	}
	if events[0].TenantID != "acme" || events[0].ThreadID != "ci-thread-9" || events[0].ActorID != "ci-bot" {
		t.Fatalf("trace attribution: %+v", events[0])
	}
}

func TestRun_defaultsWhenFlagsOmitted(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "run-defaults.db")
	root := runProjRoot(t)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"run", "workflow/demo",
		"--project", root,
		"--state", db,
		"--input", "topic=defaults",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	runID := extractRunID(out.String())

	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != state.DefaultTenantID || got.ThreadID != state.DefaultThreadID || got.ActorID != state.DefaultActorID {
		t.Fatalf("defaults: %+v", got)
	}
}

func TestRun_helpMentionsAttributionGuardrails(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"run", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "tenant-id") || !strings.Contains(text, "Never rely on") {
		t.Fatalf("help missing attribution guidance:\n%s", text)
	}
}
