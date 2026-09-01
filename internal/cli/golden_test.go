package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/apply"
	"github.com/Terfyn/terfyn/internal/plan"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
	"gopkg.in/yaml.v3"
)

// GO_UPDATE_GOLDEN=1 rewrites golden files under testdata/golden (design doc §17.3, issue #31).
const envUpdateGolden = "GO_UPDATE_GOLDEN"

var reStateLine = regexp.MustCompile(`(?m)^State: .*$`)

func normalizeGoldenCLIOutput(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = reStateLine.ReplaceAllString(s, "State: <STATE>")
	s = strings.TrimRight(s, "\n") + "\n"
	return s
}

func goldenFile(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", "golden", name)
}

func assertGoldenOutput(t *testing.T, goldenName, got string) {
	t.Helper()
	path := goldenFile(t, goldenName)
	got = normalizeGoldenCLIOutput(got)

	if os.Getenv(envUpdateGolden) == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
		return
	}

	wantRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set %s=1 to create)", path, err, envUpdateGolden)
	}
	want := normalizeGoldenCLIOutput(string(wantRaw))
	if got != want {
		t.Fatalf("golden mismatch %s\n--- got ---\n%s--- want ---\n%s", path, got, want)
	}
}

func TestGolden_validate_ok_table(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"validate", "--project", testdataPath(t, "validate_ok"), "--no-color"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertGoldenOutput(t, "validate_ok.table.golden.txt", out.String())
}

func TestGolden_validate_lint_sensitive_table(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"validate", "--project", testdataPath(t, "validate_lint_sensitive"), "--no-color"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertGoldenOutput(t, "validate_lint_sensitive.table.golden.txt", out.String())
}

func TestGolden_validate_unknown_agent_table(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"validate", "--project", testdataPath(t, "validate_unknown_agent"), "--no-color"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v\n%s", ExitCodeOf(err), err, out.String())
	}
	assertGoldenOutput(t, "validate_unknown_agent.table.golden.txt", err.Error()+"\n")
}

func TestGolden_validate_effect_unpermitted_table(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"validate", "--project", testdataPath(t, "validate_effect_unpermitted"), "--no-color"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v\n%s", ExitCodeOf(err), err, out.String())
	}
	assertGoldenOutput(t, "validate_effect_unpermitted.table.golden.txt", err.Error()+"\n")
}

func TestGolden_plan_first_table(t *testing.T) {
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "golden-plan1.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertGoldenOutput(t, "plan_first.table.golden.txt", out.String())
}

func TestGolden_plan_policy_compile_table(t *testing.T) {
	root := t.TempDir()
	copyPolicyCompileFixture(t, root)
	db := filepath.Join(t.TempDir(), "golden-policy-compile.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertGoldenOutput(t, "plan_policy_compile.table.golden.txt", out.String())
}

func TestGolden_plan_noop_after_apply_table(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "golden-plan2.db")

	g := &Global{ProjectRoot: root}
	graph, _, err := prepareProjectGraph(g)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	pl, err := plan.NewPlanner(st).ComputePlan(ctx, "local", graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := apply.NewApplier(st).ApplyPlan(ctx, "local", graph, pl, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertGoldenOutput(t, "plan_noop.table.golden.txt", out.String())
}

func TestGolden_plan_risk_categories_table(t *testing.T) {
	root := t.TempDir()
	copyRiskCategoriesFixture(t, root)
	db := filepath.Join(t.TempDir(), "golden-plan-risk.db")
	applyProjectGraph(t, root, db)
	mutateRiskCategories(t, root)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertGoldenOutput(t, "plan_risk_categories.table.golden.txt", out.String())
}

func TestGolden_plan_risk_categories_json_risk(t *testing.T) {
	root := t.TempDir()
	copyRiskCategoriesFixture(t, root)
	db := filepath.Join(t.TempDir(), "golden-plan-risk-json.db")
	applyProjectGraph(t, root, db)
	mutateRiskCategories(t, root)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--project", root, "--state", db, "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v\nbody=%s", err, out.String())
	}
	subset := map[string]any{
		"risk":      payload["risk"],
		"riskItems": payload["riskItems"],
	}
	b, err := json.MarshalIndent(subset, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenOutput(t, "plan_risk_categories.json.golden.txt", string(b)+"\n")
}

func TestGolden_plan_risk_categories_yaml_risk(t *testing.T) {
	root := t.TempDir()
	copyRiskCategoriesFixture(t, root)
	db := filepath.Join(t.TempDir(), "golden-plan-risk-yaml.db")
	applyProjectGraph(t, root, db)
	mutateRiskCategories(t, root)

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--project", root, "--state", db, "-o", "yaml"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var payload plan.RiskExport
	if err := yaml.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("yaml: %v\nbody=%s", err, out.String())
	}
	if len(payload.RiskItems) == 0 {
		t.Fatalf("yaml missing riskItems:\n%s", out.String())
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(payload); err != nil {
		t.Fatal(err)
	}
	_ = enc.Close()
	assertGoldenOutput(t, "plan_risk_categories.yaml.golden.txt", buf.String())
}

func copyEffectBoundFixture(t *testing.T, dstDir string) {
	t.Helper()
	copyFixtureDir(t, dstDir, "plan_effect_bound")
}

func TestGolden_plan_effect_bound_table(t *testing.T) {
	root := t.TempDir()
	copyEffectBoundFixture(t, root)
	db := filepath.Join(t.TempDir(), "golden-plan-effect-bound.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertGoldenOutput(t, "plan_effect_bound.table.golden.txt", out.String())
}

func TestGolden_plan_effect_bound_json(t *testing.T) {
	root := t.TempDir()
	copyEffectBoundFixture(t, root)
	db := filepath.Join(t.TempDir(), "golden-plan-effect-bound-json.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--project", root, "--state", db, "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v\nbody=%s", err, out.String())
	}
	subset := map[string]any{
		"risk":            payload["risk"],
		"riskItems":       payload["riskItems"],
		"effectBound":     payload["effectBound"],
		"capabilityDelta": payload["capabilityDelta"],
		"effectDelta":     payload["effectDelta"],
		"authority":       payload["authority"],
	}
	b, err := json.MarshalIndent(subset, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenOutput(t, "plan_effect_bound.json.golden.txt", string(b)+"\n")
}

func TestGolden_plan_effect_bound_yaml(t *testing.T) {
	root := t.TempDir()
	copyEffectBoundFixture(t, root)
	db := filepath.Join(t.TempDir(), "golden-plan-effect-bound-yaml.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--project", root, "--state", db, "-o", "yaml"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var payload plan.RiskExport
	if err := yaml.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("yaml: %v\nbody=%s", err, out.String())
	}
	if len(payload.EffectBound) == 0 {
		t.Fatalf("yaml missing effectBound:\n%s", out.String())
	}
	if payload.Authority == nil {
		t.Fatalf("yaml missing authority:\n%s", out.String())
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(payload); err != nil {
		t.Fatal(err)
	}
	_ = enc.Close()
	assertGoldenOutput(t, "plan_effect_bound.yaml.golden.txt", buf.String())
}
