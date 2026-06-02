package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestRun_safetyOnlyDenial_exit5(t *testing.T) {
	db := filepath.Join(t.TempDir(), "run-safety.db")
	root := runSafetyRoot(t)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"run", "workflow/echo",
		"--project", root,
		"--state", db,
		"--input", "topic=x",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected safety-derived policy denial")
	}
	if ExitCodeOf(err) != ExitPolicyDenied {
		t.Fatalf("exit=%d want %d err=%v", ExitCodeOf(err), ExitPolicyDenied, err)
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

func TestRun_policyDenial_exit5(t *testing.T) {
	db := filepath.Join(t.TempDir(), "run-pol.db")
	root := runPolicyRoot(t)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"run", "workflow/gated",
		"--project", root,
		"--state", db,
		"--input", "topic=x",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected policy denial")
	}
	if ExitCodeOf(err) != ExitPolicyDenied {
		t.Fatalf("exit=%d want %d err=%v", ExitCodeOf(err), ExitPolicyDenied, err)
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
	if ExitCodeOf(err) != ExitGenericFailure {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
}
