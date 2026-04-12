package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestInspect_json_shape(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"inspect", "-o", "json", "Policy/default", "--project", testdataPath(t, "validate_ok")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var top map[string]any
	if err := json.Unmarshal(out.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	if top["environment"] != "(none)" {
		t.Fatalf("environment %v", top["environment"])
	}
	res, ok := top["resource"].(map[string]any)
	if !ok {
		t.Fatalf("resource: %v", top["resource"])
	}
	if res["kind"] != "Policy" {
		t.Fatalf("kind: %v", res["kind"])
	}
	meta, _ := res["metadata"].(map[string]any)
	if meta["name"] != "default" {
		t.Fatalf("metadata: %v", meta)
	}
}

func TestInspect_yaml_containsKind(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"inspect", "-o", "yaml", "Tool/helper", "--project", testdataPath(t, "validate_ok")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "kind: Tool") || !strings.Contains(s, "name: helper") {
		t.Fatalf("got:\n%s", s)
	}
}

func TestInspect_policy_envOverlay_json(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"inspect", "-o", "json", "-e", "staging", "Policy/default", "--project", testdataPath(t, "validate_ok")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var top map[string]any
	if err := json.Unmarshal(out.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	if top["environment"] != "staging" {
		t.Fatalf("environment %v", top["environment"])
	}
	res := top["resource"].(map[string]any)
	spec := res["spec"].(map[string]any)
	exec := spec["execution"].(map[string]any)
	// JSON numbers decode as float64
	if exec["maxWallClockSeconds"].(float64) != 600 {
		t.Fatalf("staging overlay: execution=%v", exec)
	}

	ResetGlobalsForTest()
	cmd = NewRootCmd()
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"inspect", "-o", "json", "Policy/default", "--project", testdataPath(t, "validate_ok")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	res = top["resource"].(map[string]any)
	spec = res["spec"].(map[string]any)
	exec = spec["execution"].(map[string]any)
	if exec["maxWallClockSeconds"].(float64) != 300 {
		t.Fatalf("base policy: execution=%v", exec)
	}
}

func TestInspect_table_headerAndJSONBody(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"inspect", "Workflow/demo", "--project", testdataPath(t, "validate_ok")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.HasPrefix(s, "Resource: Workflow/demo\nEnvironment: (none)\n\n") {
		t.Fatalf("header:\n%s", s)
	}
	if !strings.Contains(s, `"kind": "Workflow"`) || !strings.Contains(s, `"name": "demo"`) {
		t.Fatalf("body:\n%s", s)
	}
}

func TestInspect_kindNameCaseInsensitive(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"inspect", "-o", "json", "workflow/demo", "--project", testdataPath(t, "validate_ok")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var top map[string]any
	if err := json.Unmarshal(out.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	res := top["resource"].(map[string]any)
	if res["kind"] != "Workflow" {
		t.Fatalf("%v", res["kind"])
	}
}

func TestInspect_projectResource(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"inspect", "-o", "json", "Project/validate-ok", "--project", testdataPath(t, "validate_ok")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var top map[string]any
	if err := json.Unmarshal(out.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	res := top["resource"].(map[string]any)
	if res["kind"] != "Project" {
		t.Fatalf("%v", res["kind"])
	}
}

func TestInspect_unknownResource_exit2(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"inspect", "Agent/missing", "--project", testdataPath(t, "validate_ok")})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("code=%d err=%v", ExitCodeOf(err), err)
	}
}

func TestInspect_wrongArgCount_exit2(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"inspect", "--project", testdataPath(t, "validate_ok")})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("code=%d err=%v", ExitCodeOf(err), err)
	}
}

func TestInspect_projectNameMismatch_exit2(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"inspect", "Project/wrong-name", "--project", testdataPath(t, "validate_ok")})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("code=%d err=%v", ExitCodeOf(err), err)
	}
}
