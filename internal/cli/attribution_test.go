package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

func TestResolveRunAttributionFlags_envOverrides(t *testing.T) {
	t.Setenv(EnvTenantID, "env-tenant")
	t.Setenv(EnvThreadID, "env-thread")
	t.Setenv(EnvActorID, "env-actor")

	got := resolveRunAttributionFields("", "", "", "", "", "", "", false)
	if got.TenantID != "env-tenant" || got.ThreadID != "env-thread" || got.ActorID != "env-actor" {
		t.Fatalf("env overrides: %+v", got)
	}

	got = resolveRunAttributionFields("flag-tenant", "", "", "", "", "", "", false)
	if got.TenantID != "flag-tenant" {
		t.Fatalf("flag wins: %+v", got)
	}
}

func TestWarnAttributionDefaults(t *testing.T) {
	var buf bytes.Buffer
	warnAttributionDefaults(&buf, state.RunAttribution{})
	if !strings.Contains(buf.String(), "warning:") || !strings.Contains(buf.String(), "tenant-1") {
		t.Fatalf("warn: %q", buf.String())
	}
	buf.Reset()
	warnAttributionDefaults(&buf, state.RunAttribution{TenantID: "t", ThreadID: "th", ActorID: "a"})
	if buf.Len() != 0 {
		t.Fatalf("no warn expected: %q", buf.String())
	}
}

func TestRun_requireAttributionRejectsDefaults(t *testing.T) {
	root := runProjRoot(t)
	db := t.TempDir() + "/req.db"

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{
		"run", "workflow/demo",
		"--project", root,
		"--state", db,
		"--input", "topic=x",
		"--require-attribution",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "attribution required") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_requireAttributionViaEnv(t *testing.T) {
	t.Setenv(EnvRequireAttribution, "1")
	root := runProjRoot(t)
	db := t.TempDir() + "/req-env.db"

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"run", "workflow/demo",
		"--project", root,
		"--state", db,
		"--input", "topic=x",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "attribution required") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_warnsOnDefaultAttribution(t *testing.T) {
	root := runProjRoot(t)
	db := t.TempDir() + "/warn.db"

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{
		"run", "workflow/demo",
		"--project", root,
		"--state", db,
		"--input", "topic=warn-test",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "warning:") {
		t.Fatalf("stderr = %q", errBuf.String())
	}
}

func TestEnvTruthy(t *testing.T) {
	t.Setenv("AGENTCTL_TEST_TRUTHY", "yes")
	if !envTruthy("AGENTCTL_TEST_TRUTHY") {
		t.Fatal("expected truthy")
	}
	t.Setenv("AGENTCTL_TEST_TRUTHY", "0")
	if envTruthy("AGENTCTL_TEST_TRUTHY") {
		t.Fatal("expected false")
	}
}
