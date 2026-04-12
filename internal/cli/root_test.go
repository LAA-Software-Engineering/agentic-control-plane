package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestRootHelp_listsGlobalFlags(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, sub := range []string{"diff", "inspect"} {
		if !strings.Contains(out, sub) {
			t.Fatalf("help should mention %q subcommand:\n%s", sub, out)
		}
	}
	for _, flag := range []string{"--env", "-e", "--output", "-o", "--project", "--state", "--no-color"} {
		if !strings.Contains(out, flag) {
			t.Fatalf("help output missing %q\n%s", flag, out)
		}
	}
}

func TestVersion_JSON_stableKeys(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"-o", "json", "version"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	raw := bytes.TrimSpace(buf.Bytes())
	if !json.Valid(raw) {
		t.Fatalf("invalid json: %s", raw)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 {
		t.Fatalf("want single top-level key, got %v", m)
	}
	if _, ok := m["version"]; !ok {
		t.Fatalf("missing version key: %v", m)
	}
}

func TestInvalidOutput_exitCode2(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"-o", "nope", "version"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("code=%d err=%v", ExitCodeOf(err), err)
	}
}
