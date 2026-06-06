package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/project"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func scaffoldFixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "project.yaml"), `apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: scaffold-test
spec:
  imports:
    - ./policies/default.yaml
  defaults:
    policy: default
    model: mock/gpt-4
  providers:
    models:
      mock:
        type: mock
`)
	writeFile(t, filepath.Join(root, "policies", "default.yaml"), `apiVersion: agentic.dev/v0
kind: Policy
metadata:
  name: default
spec:
  preset: shell_safe
`)
	return root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateTool_http(t *testing.T) {
	root := scaffoldFixtureRoot(t)
	plan, err := GenerateTool(Options{ProjectRoot: root}, "webhook", ToolKindHTTP)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ResourceKind != KindTool || plan.ResourceName != "webhook" {
		t.Fatalf("plan = %+v", plan)
	}
	if !plan.ImportAppended {
		t.Fatal("expected import append")
	}
	if !strings.Contains(string(plan.ResourceYAML), "type: http") {
		t.Fatalf("yaml: %s", plan.ResourceYAML)
	}
	if err := Apply(plan, Options{ProjectRoot: root}); err != nil {
		t.Fatal(err)
	}
	if _, err := project.LoadProject(root); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateTool_kindsValidate(t *testing.T) {
	kinds := []string{ToolKindNative, ToolKindHTTP, ToolKindMock, ToolKindMCP}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			root := scaffoldFixtureRoot(t)
			name := "k_" + kind
			plan, err := GenerateTool(Options{ProjectRoot: root}, name, kind)
			if err != nil {
				t.Fatal(err)
			}
			if err := Apply(plan, Options{ProjectRoot: root}); err != nil {
				t.Fatal(err)
			}
			g, err := project.LoadProject(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := g.Tools[name]; !ok {
				t.Fatalf("missing tool %s", name)
			}
			if err := spec.ValidateProjectGraph(g, root); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGeneratePolicy_presets(t *testing.T) {
	for _, preset := range spec.BuiltinPresetNames() {
		t.Run(preset, func(t *testing.T) {
			root := scaffoldFixtureRoot(t)
			name := "pol_" + preset
			plan, err := GeneratePolicy(Options{ProjectRoot: root}, name, preset)
			if err != nil {
				t.Fatal(err)
			}
			if err := Apply(plan, Options{ProjectRoot: root}); err != nil {
				t.Fatal(err)
			}
			g, err := project.LoadProject(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := spec.ValidateProjectGraph(g, root); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGenerateWorkflow_usesProjectDefaultPolicy(t *testing.T) {
	root := scaffoldFixtureRoot(t)
	plan, err := GenerateWorkflow(Options{ProjectRoot: root}, "flow")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plan.ResourceYAML), "policy: default") {
		t.Fatalf("yaml: %s", plan.ResourceYAML)
	}
}

func TestGenerateAgent(t *testing.T) {
	root := scaffoldFixtureRoot(t)
	plan, err := GenerateAgent(Options{ProjectRoot: root}, "helper")
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(plan, Options{ProjectRoot: root}); err != nil {
		t.Fatal(err)
	}
	g, err := project.LoadProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.ValidateProjectGraph(g, root); err != nil {
		t.Fatal(err)
	}
}

func TestGenerate_duplicateName(t *testing.T) {
	root := scaffoldFixtureRoot(t)
	_, err := GeneratePolicy(Options{ProjectRoot: root}, "default", spec.PresetStrict)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
}

func TestGenerate_duplicateFile(t *testing.T) {
	root := scaffoldFixtureRoot(t)
	writeFile(t, filepath.Join(root, "tools", "exists.yaml"), `apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: exists
spec:
  type: native
  safety:
    sideEffects: false
`)
	_, err := GenerateTool(Options{ProjectRoot: root}, "exists", ToolKindNative)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
}

func TestApply_rollbackOnInjectedFailure(t *testing.T) {
	root := scaffoldFixtureRoot(t)
	projPath := filepath.Join(root, "project.yaml")
	before, err := os.ReadFile(projPath)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := GenerateTool(Options{ProjectRoot: root}, "rollback", ToolKindNative)
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	err = Apply(plan, Options{ProjectRoot: root, TestFailAfter: &zero})
	if err == nil {
		t.Fatal("expected injected failure")
	}

	afterProj, err := os.ReadFile(projPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterProj) != string(before) {
		t.Fatal("project.yaml changed after rollback")
	}
	if _, err := os.Stat(plan.ResourcePath); !os.IsNotExist(err) {
		t.Fatalf("resource file should not exist: %v", err)
	}
}

func TestAppendProjectImport_idempotent(t *testing.T) {
	src := []byte(`apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: p
spec:
  imports:
    - ./tools/a.yaml
`)
	out, added, err := appendProjectImport(src, "./tools/a.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if added {
		t.Fatal("expected no append for duplicate")
	}
	if string(out) != string(src) {
		t.Fatalf("changed:\n%s", out)
	}

	out2, added2, err := appendProjectImport(src, "./tools/b.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !added2 {
		t.Fatal("expected append")
	}
	if !strings.Contains(string(out2), "./tools/b.yaml") {
		t.Fatalf("missing import: %s", out2)
	}
}

func TestValidateResourceName(t *testing.T) {
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"foo", false},
		{"", true},
		{"a/b", true},
		{"..", true},
	}
	for _, tc := range cases {
		err := ValidateResourceName(tc.name)
		if tc.wantErr && err == nil {
			t.Fatalf("%q: want error", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Fatalf("%q: %v", tc.name, err)
		}
	}
}

func TestGenerateTool_dryRunDoesNotWrite(t *testing.T) {
	root := scaffoldFixtureRoot(t)
	plan, err := GenerateTool(Options{ProjectRoot: root, DryRun: true}, "dry", ToolKindNative)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(plan, Options{ProjectRoot: root, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plan.ResourcePath); !os.IsNotExist(err) {
		t.Fatal("file should not exist after dry-run apply")
	}
}
