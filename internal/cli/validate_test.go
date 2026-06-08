package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func testdataPath(t *testing.T, parts ...string) string {
	t.Helper()
	return filepath.Join(append([]string{"testdata"}, parts...)...)
}

func TestValidate_successWithEnv(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"validate", "--project", testdataPath(t, "validate_ok"), "-e", "staging"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Environment: staging") {
		t.Fatal(out.String())
	}
}

func TestValidate_successJSON(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-o", "json", "validate", "--project", testdataPath(t, "validate_ok")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Project       string `json:"project"`
		Valid         bool   `json:"valid"`
		ResourceCount int    `json:"resourceCount"`
		Message       string `json:"message"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Valid || payload.Project != "validate-ok" || payload.ResourceCount != 4 {
		t.Fatalf("%+v", payload)
	}
	if payload.Message != "Validation successful" {
		t.Fatal(payload.Message)
	}
}

func TestValidate_badFixture_exit2(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"validate", "--project", testdataPath(t, "validate_bad")})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("got code %d err %v", ExitCodeOf(err), err)
	}
}

func TestValidate_policyLint_table(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"validate", "--project", testdataPath(t, "validate_lint_sensitive"), "--no-color"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "Policy lint") {
		t.Fatalf("expected policy lint section: %s", s)
	}
	if !strings.Contains(s, "ungated") && !strings.Contains(s, "explicit approval rule") {
		t.Fatalf("expected sensitive tool finding: %s", s)
	}
}

func TestValidate_policyLint_strictExit2(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"validate", "--strict", "--project", testdataPath(t, "validate_lint_sensitive")})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("got code %d err %v", ExitCodeOf(err), err)
	}
}

func TestValidate_policyLint_json(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-o", "json", "validate", "--project", testdataPath(t, "validate_lint_switch")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Valid      bool `json:"valid"`
		PolicyLint []struct {
			Rule     string `json:"rule"`
			Severity string `json:"severity"`
		} `json:"policyLint"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Valid {
		t.Fatal("expected valid project")
	}
	var found bool
	for _, f := range payload.PolicyLint {
		if f.Rule == "invalid_switch_target" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("policyLint = %+v", payload.PolicyLint)
	}
}

func TestValidate_policyLint_strictJSON(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"-o", "json", "validate", "--strict", "--project", testdataPath(t, "validate_lint_sensitive")})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
	var payload struct {
		Valid         bool `json:"valid"`
		ResourceCount int  `json:"resourceCount"`
		PolicyLint    []struct {
			Severity string `json:"severity"`
		} `json:"policyLint"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Valid {
		t.Fatal("expected valid=false")
	}
	if payload.ResourceCount == 0 {
		t.Fatal("expected resourceCount in strict failure payload")
	}
	if len(payload.PolicyLint) == 0 {
		t.Fatal("expected policyLint entries")
	}
}

func TestValidate_policyLint_yaml(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-o", "yaml", "validate", "--project", testdataPath(t, "validate_lint_sensitive")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "policyLint:") {
		t.Fatalf("expected policyLint in yaml: %s", out.String())
	}
	if !strings.Contains(out.String(), "ungated_sensitive_tool") {
		t.Fatalf("expected rule in yaml: %s", out.String())
	}
}

func TestValidate_validateOk_strictPasses(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"validate", "--strict", "--project", testdataPath(t, "validate_ok")})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("validate_ok should pass strict lint: %v", err)
	}
}

func TestValidate_mcpDiscoveryWarning_advisory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "project.yaml"), `apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: mcp-warn
spec:
  imports:
    - tools/
  state:
    backend: sqlite
    dsn: .agentic/state.db
`)
	writeFile(t, filepath.Join(root, "tools", "mc.yaml"), `apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: mc
spec:
  type: mcp
  mcp:
    transport: stdio
    command: /nonexistent/mcp-binary-for-validate-test
`)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-o", "json", "validate", "--project", root, "--no-color"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Valid                bool `json:"valid"`
		MCPDiscoveryWarnings []struct {
			Tool    string `json:"tool"`
			Message string `json:"message"`
		} `json:"mcpDiscoveryWarnings"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Valid {
		t.Fatalf("validate should succeed: %s", out.String())
	}
	if len(payload.MCPDiscoveryWarnings) != 1 {
		t.Fatalf("expected MCP discovery warning: %s", out.String())
	}
	if payload.MCPDiscoveryWarnings[0].Tool != "mc" {
		t.Fatalf("tool: %+v", payload.MCPDiscoveryWarnings[0])
	}
}
