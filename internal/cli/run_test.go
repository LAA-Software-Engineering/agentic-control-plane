package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/engine"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/project"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime/local"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

func runProjRoot(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "runtime", "local", "testdata", "runproj")
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func runPolicyRoot(t *testing.T) string {
	t.Helper()
	p := filepath.Join("testdata", "run_policy")
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func runSafetyRoot(t *testing.T) string {
	t.Helper()
	p := filepath.Join("testdata", "run_safety")
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestRun_demo_integration_succeeds(t *testing.T) {
	db := filepath.Join(t.TempDir(), "run-cli.db")
	root := runProjRoot(t)

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"run", "workflow/demo",
		"--project", root,
		"-e", "staging",
		"--state", db,
		"--input", "topic=from-cli",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "Run ID:") || !strings.Contains(s, "Status: succeeded") {
		t.Fatalf("unexpected output:\n%s", s)
	}
}

func TestRun_safetyOnly_interruptsAwaitingHitl(t *testing.T) {
	db := filepath.Join(t.TempDir(), "run-safety.db")
	root := runSafetyRoot(t)

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"run", "workflow/echo",
		"--project", root,
		"--state", db,
		"--input", "topic=x",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "Status: interrupted") {
		t.Fatalf("expected interrupted:\n%s", out.String())
	}
}

func TestRun_safetyOnly_withApprove_succeeds(t *testing.T) {
	db := filepath.Join(t.TempDir(), "run-safety-ok.db")
	root := runSafetyRoot(t)

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"run", "workflow/echo",
		"--project", root,
		"--state", db,
		"--input", "topic=x",
		"--approve", "tool.helper.echo",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Status: succeeded") {
		t.Fatalf("output:\n%s", out.String())
	}
}

func TestRun_policyGated_interruptThenResumeApprove(t *testing.T) {
	db := filepath.Join(t.TempDir(), "run-pol.db")
	root := runPolicyRoot(t)

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"run", "workflow/gated",
		"--project", root,
		"--state", db,
		"--input", "topic=x",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("first run: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Status: interrupted") {
		t.Fatalf("expected interrupted:\n%s", out.String())
	}
	runID := ""
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Run ID:") {
			runID = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "Run ID:"))
		}
	}
	if runID == "" {
		t.Fatal("missing run id")
	}

	out.Reset()
	cmd2 := NewRootCmd()
	cmd2.SetOut(&out)
	cmd2.SetErr(&out)
	cmd2.SetArgs([]string{
		"run", "--resume", runID,
		"--project", root,
		"--state", db,
		"--decision", "approve",
	})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("resume: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Status: succeeded") {
		t.Fatalf("expected succeeded:\n%s", out.String())
	}
}

func TestRun_withApprove_succeeds(t *testing.T) {
	db := filepath.Join(t.TempDir(), "run-ok.db")
	root := runPolicyRoot(t)

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"run", "workflow/gated",
		"--project", root,
		"--state", db,
		"--input", "topic=x",
		"--approve", "tool.helper.echo",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Status: succeeded") {
		t.Fatalf("output:\n%s", out.String())
	}
}

func TestRun_badWorkflowRef_exit2(t *testing.T) {
	db := filepath.Join(t.TempDir(), "run-bad.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"run", "demo", "--project", ".", "--state", db})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
}

func TestRun_badInputPair_exit2(t *testing.T) {
	db := filepath.Join(t.TempDir(), "run-inp.db")
	root := runProjRoot(t)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"run", "workflow/demo", "--project", root, "--state", db, "--input", "notakeyvalue"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
}

func TestRun_inputFile_succeeds(t *testing.T) {
	db := filepath.Join(t.TempDir(), "run-file.db")
	root := runProjRoot(t)
	f := filepath.Join(t.TempDir(), "in.json")
	if err := os.WriteFile(f, []byte(`{"topic":"from-file"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"run", "workflow/demo",
		"--project", root,
		"-e", "staging",
		"--state", db,
		"--input-file", f,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "succeeded") {
		t.Fatal(out.String())
	}
}

func TestRun_resume_missingRun_exit1(t *testing.T) {
	db := filepath.Join(t.TempDir(), "resume-missing.db")
	root := runProjRoot(t)

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"run", "--resume", "does-not-exist",
		"--project", root,
		"--state", db,
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitGenericFailure {
		t.Fatalf("exit=%d err=%v out=%s", ExitCodeOf(err), err, out.String())
	}
}

func TestRun_resume_withWorkflowArg_exit2(t *testing.T) {
	db := filepath.Join(t.TempDir(), "resume-bad-args.db")
	root := runProjRoot(t)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetArgs([]string{
		"run", "workflow/demo", "--resume", "some-id",
		"--project", root,
		"--state", db,
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
}

func TestRun_resume_happyPath(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "resume-happy.db")
	root := runProjRoot(t)

	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph, err := project.LoadProject(root)
	if err != nil {
		t.Fatal(err)
	}
	spec.NormalizeProjectGraph(graph)
	graph, err = local.ApplyEnvironment(graph, "staging")
	if err != nil {
		t.Fatal(err)
	}
	wf := graph.Workflows["demo"]
	wfHash, err := plan.WorkflowSpecHash(wf)
	if err != nil {
		t.Fatal(err)
	}

	runID := "cli-resume-1"
	started := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "demo", Env: "dev", Status: state.RunStatusRunning,
		StartedAt: started, InputJSON: `{"topic":"cli-resume"}`, TotalCostUSD: 0,
		WorkflowSpecHash: wfHash, EnvironmentName: "staging",
	}); err != nil {
		t.Fatal(err)
	}

	var input map[string]any
	if err := json.Unmarshal([]byte(`{"topic":"cli-resume"}`), &input); err != nil {
		t.Fatal(err)
	}
	idx := 0
	ex := &engine.Executor{
		Graph: graph, ProjectRoot: root,
		Tools: tools.NewRegistry(graph), Models: models.NewRegistry(graph),
		Store: st, Trace: trace.NewRecorder(st),
		Now: func() time.Time { return started },
	}
	if err := ex.Run(ctx, engine.RunInput{
		RunID: runID, WorkflowName: "demo", Env: "dev", StartedAt: started, Input: input,
		InterruptAfterStepIndex: &idx,
	}); !errors.Is(err, engine.ErrInterrupted) {
		t.Fatalf("interrupt: %v", err)
	}

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"run", "--resume", runID,
		"--project", root,
		"-e", "staging",
		"--state", db,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("resume: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "succeeded") {
		t.Fatalf("output:\n%s", out.String())
	}
	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.RunStatusSucceeded {
		t.Fatalf("status %q", got.Status)
	}
}
