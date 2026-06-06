package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/scaffold"
)

func newTestProjectRoot(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	name := "proj"

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"init", name, "--parent-dir", parent})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, name)
}

func TestNew_tool_http_thenValidate(t *testing.T) {
	root := newTestProjectRoot(t)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"new", "tool", "foo", "--kind", "http", "--project", root})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	ResetGlobalsForTest()
	v := NewRootCmd()
	v.SetOut(io.Discard)
	v.SetErr(io.Discard)
	v.SetArgs([]string{"validate", "--project", root})
	if err := v.Execute(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "./tools/foo.yaml") {
		t.Fatalf("imports: %s", data)
	}
}

func TestNew_duplicateNameErrors(t *testing.T) {
	root := newTestProjectRoot(t)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"new", "tool", "helper", "--project", root})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for existing tool name")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
}

func TestNew_dryRunDoesNotWrite(t *testing.T) {
	root := newTestProjectRoot(t)
	before, err := os.ReadFile(filepath.Join(root, "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"new", "tool", "drytool", "--dry-run", "--project", root})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Dry run:") {
		t.Fatalf("output: %s", out.String())
	}
	if !strings.Contains(out.String(), "./tools/drytool.yaml") {
		t.Fatalf("output: %s", out.String())
	}

	after, err := os.ReadFile(filepath.Join(root, "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("project.yaml changed during dry-run")
	}
	if _, err := os.Stat(filepath.Join(root, "tools", "drytool.yaml")); !os.IsNotExist(err) {
		t.Fatal("resource file created during dry-run")
	}
}

func TestNew_policy_workflow_agent(t *testing.T) {
	root := newTestProjectRoot(t)
	cases := []struct {
		args []string
		file string
	}{
		{[]string{"new", "policy", "strictpol", "--preset", "strict", "--project", root}, "policies/strictpol.yaml"},
		{[]string{"new", "workflow", "wf2", "--project", root}, "workflows/wf2.yaml"},
		{[]string{"new", "agent", "bot", "--project", root}, "agents/bot.yaml"},
	}
	for _, tc := range cases {
		ResetGlobalsForTest()
		cmd := NewRootCmd()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(tc.args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if _, err := os.Stat(filepath.Join(root, tc.file)); err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
	}

	ResetGlobalsForTest()
	v := NewRootCmd()
	v.SetOut(io.Discard)
	v.SetErr(io.Discard)
	v.SetArgs([]string{"validate", "--project", root})
	if err := v.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestNew_rollbackOnFailure(t *testing.T) {
	root := newTestProjectRoot(t)
	projPath := filepath.Join(root, "project.yaml")
	before, err := os.ReadFile(projPath)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := scaffold.GenerateTool(scaffold.Options{ProjectRoot: root}, "rb", scaffold.ToolKindNative)
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	if err := scaffold.Apply(plan, scaffold.Options{ProjectRoot: root, TestFailAfter: &zero}); err == nil {
		t.Fatal("expected failure")
	}

	after, err := os.ReadFile(projPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("project unchanged after rollback")
	}
}
