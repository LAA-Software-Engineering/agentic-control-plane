package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareResolvedConfig_userLocalPrecedence(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(root, "project.yaml"), `apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: demo
spec:
  defaults:
    model: project-model
  state:
    backend: sqlite
    dsn: .agentic/state.db
`)
	writeFile(t, filepath.Join(home, ".config", "agentctl", "config.yaml"), `state:
  dsn: /tmp/user-global-state.db
`)

	ResetGlobalsForTest()
	global = Global{ProjectRoot: root}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	rc, err := prepareResolvedConfig(&global)
	if err != nil {
		t.Fatal(err)
	}
	if rc.Graph().Spec.Defaults.Model != "project-model" {
		t.Fatalf("model = %q, want project-model", rc.Graph().Spec.Defaults.Model)
	}
	if !strings.HasSuffix(rc.StatePath(), filepath.Join(".agentic", "state.db")) {
		t.Fatalf("project state should win, got %q", rc.StatePath())
	}
}

func TestPrepareResolvedConfig_unknownProjectField(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "project.yaml"), `apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: demo
spec:
  defualts:
    model: x
`)
	ResetGlobalsForTest()
	global = Global{ProjectRoot: root}
	_, err := prepareResolvedConfig(&global)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "defualts") {
		t.Fatalf("want typo in error: %v", err)
	}
}

func TestRun_afterValidate_stateDrift_exit3(t *testing.T) {
	root := runProjRoot(t)
	db := filepath.Join(t.TempDir(), "run.db")
	db2 := filepath.Join(t.TempDir(), "run-other.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"validate", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	ResetGlobalsForTest()
	var errBuf bytes.Buffer
	cmd = NewRootCmd()
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{
		"run", "workflow/demo",
		"--project", root,
		"--state", db2,
		"--input", "topic=drift-test",
	})
	defer func() { _ = os.Remove(config.SnapshotPath(root)) }()

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected run to fail with config drift")
	}
	if ExitCodeOf(err) != ExitPlanApplyConflict {
		t.Fatalf("exit code = %d, want %d; err=%v", ExitCodeOf(err), ExitPlanApplyConflict, err)
	}
	if !strings.Contains(err.Error(), "resolved config") {
		t.Fatalf("want drift message, got: %v", err)
	}
}

func TestRun_resolvedConfigDrift_exit3(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "project.yaml"), `apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: demo
spec:
  state:
    backend: sqlite
    dsn: .agentic/state.db
`)
	ResetGlobalsForTest()
	global = Global{ProjectRoot: root}
	rc, err := prepareResolvedConfig(&global)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.WriteSnapshot(rc); err != nil {
		t.Fatal(err)
	}

	global.StatePath = filepath.Join(root, "other.db")
	rc2, err := prepareResolvedConfig(&global)
	if err != nil {
		t.Fatal(err)
	}
	err = config.AssertSnapshotMatchesStored(rc2)
	if err == nil {
		t.Fatal("expected drift")
	}
	if !errors.Is(err, config.ErrResolvedConfigDrift) {
		t.Fatalf("want ErrResolvedConfigDrift, got %v", err)
	}
	if code := ExitCodeOf(NewExitError(ExitPlanApplyConflict, err)); code != ExitPlanApplyConflict {
		t.Fatalf("exit code = %d, want %d", code, ExitPlanApplyConflict)
	}
}
