package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/config"
	"github.com/Terfyn/terfyn/internal/policy"
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
	// defaults are .agent project source (win over overlays); state is operator-config with no project
	// source layer (ADR 007), so the user-global overlay supplies it.
	writeFile(t, filepath.Join(root, "main.agent"), `defaults {
    model mock/project-model
}

agent assistant {
    model mock/default
}
`)
	writeFile(t, filepath.Join(home, ".config", "terfyn", "config.yaml"), `state:
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
	if rc.Graph().Spec.Defaults.Model != "mock/project-model" {
		t.Fatalf("model = %q, want mock/project-model", rc.Graph().Spec.Defaults.Model)
	}
	if !strings.HasSuffix(rc.StatePath(), "user-global-state.db") {
		t.Fatalf("user-global overlay state should apply, got %q", rc.StatePath())
	}
}

// TestPrepareResolvedConfig_rejectsYAMLProject: a project.yaml manifest is refused by the shared load
// path with the ADR 007 migrate hint (the strict unknown-field decode that this test formerly exercised
// now lives only in the retained YAML codec, tested at internal/spec).
func TestPrepareResolvedConfig_rejectsYAMLProject(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "project.yaml"), `apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: demo
`)
	ResetGlobalsForTest()
	global = Global{ProjectRoot: root}
	_, err := prepareResolvedConfig(&global)
	if err == nil {
		t.Fatal("expected a YAML-source rejection")
	}
	if !strings.Contains(err.Error(), "no longer an accepted project source") {
		t.Fatalf("want ADR 007 reject, got: %v", err)
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
	writeFile(t, filepath.Join(root, "main.agent"), `agent assistant {
    model mock/default
}
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
	writeFile(t, filepath.Join(root, "main.agent"), `policy default {
    execution {
        maxTotalCostUsd 3
    }
}

agent assistant {
    model mock/default
}
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

	policyPath := filepath.Join(root, "main.agent")
	b, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(b), "maxTotalCostUsd 3", "maxTotalCostUsd 10", 1)
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
