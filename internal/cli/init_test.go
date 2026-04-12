package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestInit_thenValidateSucceeds(t *testing.T) {
	parent := t.TempDir()
	name := "starter"

	ResetGlobalsForTest()
	icmd := NewRootCmd()
	icmd.SetOut(io.Discard)
	icmd.SetErr(io.Discard)
	icmd.SetArgs([]string{"init", name, "--parent-dir", parent})
	if err := icmd.Execute(); err != nil {
		t.Fatal(err)
	}

	proj := filepath.Join(parent, name)
	if _, err := os.Stat(filepath.Join(proj, "project.yaml")); err != nil {
		t.Fatal(err)
	}

	ResetGlobalsForTest()
	v := NewRootCmd()
	v.SetOut(io.Discard)
	v.SetErr(io.Discard)
	v.SetArgs([]string{"validate", "--project", proj})
	if err := v.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestInit_rejectsExistingDir(t *testing.T) {
	parent := t.TempDir()
	name := "dup"
	if err := os.MkdirAll(filepath.Join(parent, name), 0o755); err != nil {
		t.Fatal(err)
	}

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"init", name, "--parent-dir", parent})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestInit_rejectsBadName(t *testing.T) {
	parent := t.TempDir()

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"init", "../nope", "--parent-dir", parent})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
}
