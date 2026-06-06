package scaffold

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGolden_toolNative(t *testing.T) {
	root := scaffoldFixtureRoot(t)
	plan, err := GenerateTool(Options{ProjectRoot: root}, "echo", ToolKindNative)
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenYAML(t, "tool_native.yaml", plan.ResourceYAML)
}

func TestGolden_policyShellSafe(t *testing.T) {
	root := scaffoldFixtureRoot(t)
	plan, err := GeneratePolicy(Options{ProjectRoot: root}, "guarded", "shell_safe")
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenYAML(t, "policy_shell_safe.yaml", plan.ResourceYAML)
}

func TestGolden_workflow(t *testing.T) {
	root := scaffoldFixtureRoot(t)
	plan, err := GenerateWorkflow(Options{ProjectRoot: root}, "demo")
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenYAML(t, "workflow.yaml", plan.ResourceYAML)
}

func TestGolden_agent(t *testing.T) {
	root := scaffoldFixtureRoot(t)
	plan, err := GenerateAgent(Options{ProjectRoot: root}, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenYAML(t, "agent.yaml", plan.ResourceYAML)
}

const envUpdateGolden = "GO_UPDATE_GOLDEN"

func assertGoldenYAML(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	content := string(got)
	if os.Getenv(envUpdateGolden) == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set %s=1 to create)", path, err, envUpdateGolden)
	}
	if string(want) != content {
		t.Fatalf("golden mismatch %s\n--- got ---\n%s--- want ---\n%s", path, content, want)
	}
}
