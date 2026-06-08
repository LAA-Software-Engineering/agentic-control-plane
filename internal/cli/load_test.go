package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
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

func TestRun_policySnapshotDrift_exit3(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "project.yaml"), `apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: demo
spec:
  imports:
    - ./policy.yaml
  state:
    backend: sqlite
    dsn: .agentic/state.db
`)
	writeFile(t, filepath.Join(root, "policy.yaml"), `apiVersion: agentic.dev/v0
kind: Policy
metadata:
  name: default
spec:
  execution:
    maxTotalCostUsd: 3
`)

	ResetGlobalsForTest()
	global = Global{ProjectRoot: root}
	rc, err := prepareResolvedConfig(&global)
	if err != nil {
		t.Fatal(err)
	}
	if err := persistSnapshots(rc); err != nil {
		t.Fatal(err)
	}

	policyPath := filepath.Join(root, "policy.yaml")
	b, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(b), "maxTotalCostUsd: 3", "maxTotalCostUsd: 10", 1)
	if err := os.WriteFile(policyPath, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}

	rc2, err := prepareResolvedConfig(&global)
	if err != nil {
		t.Fatal(err)
	}
	err = assertPolicySnapshotMatches(rc2)
	if err == nil {
		t.Fatal("expected policy drift")
	}
	if !errors.Is(err, policy.ErrPolicySnapshotDrift) {
		t.Fatalf("want ErrPolicySnapshotDrift, got %v", err)
	}
	if code := ExitCodeOf(NewExitError(ExitPlanApplyConflict, err)); code != ExitPlanApplyConflict {
		t.Fatalf("exit code = %d, want %d", code, ExitPlanApplyConflict)
	}
}
