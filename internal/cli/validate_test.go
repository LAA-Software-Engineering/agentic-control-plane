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
