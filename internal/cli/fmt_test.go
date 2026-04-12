package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFmt_checkDetectsChange(t *testing.T) {
	root := testdataPath(t, "fmt_messy")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"fmt", "--check", "--project", root})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected check failure")
	}
	if ExitCodeOf(err) != ExitGenericFailure {
		t.Fatalf("code=%d err=%v", ExitCodeOf(err), err)
	}
}

func TestFmt_writeThenCheckClean(t *testing.T) {
	srcRoot := testdataPath(t, "fmt_messy")
	root := t.TempDir()
	entries, err := os.ReadDir(srcRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(srcRoot, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"fmt", "--project", root})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	ResetGlobalsForTest()
	cmd = NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"fmt", "--check", "--project", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("second check: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(root, "policy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "    name:") {
		t.Fatalf("expected 2-space indent, got:\n%s", b)
	}
}

func TestFmt_secondRunNoop(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"project.yaml", "tool.yaml", "policy.yaml"} {
		src, err := os.ReadFile(filepath.Join(testdataPath(t, "fmt_messy"), name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), src, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out1 bytes.Buffer
	cmd.SetOut(&out1)
	cmd.SetErr(&out1)
	cmd.SetArgs([]string{"fmt", "--project", root})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	ResetGlobalsForTest()
	cmd = NewRootCmd()
	var out2 bytes.Buffer
	cmd.SetOut(&out2)
	cmd.SetErr(&out2)
	cmd.SetArgs([]string{"fmt", "--project", root})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2.String(), "0 unchanged") && !strings.Contains(out2.String(), "3 unchanged") {
		t.Fatalf("expected noop summary, got:\n%s", out2.String())
	}
}

func TestFmt_jsonOutput(t *testing.T) {
	root := testdataPath(t, "fmt_messy")
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"fmt", "--check", "-o", "json", "--project", root})
	_ = cmd.Execute()
	if !strings.Contains(out.String(), `"changed"`) {
		t.Fatalf("%s", out.String())
	}
}
