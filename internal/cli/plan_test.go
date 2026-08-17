package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/apply"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"gopkg.in/yaml.v3"
)

func copyFixtureDir(t *testing.T, dstDir, fixtureName string) {
	t.Helper()
	src := filepath.Join("testdata", fixtureName)
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dstDir, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func copyPlanFixture(t *testing.T, dstDir string) {
	t.Helper()
	copyFixtureDir(t, dstDir, "plan_project")
}

func copyPolicyCompileFixture(t *testing.T, dstDir string) {
	t.Helper()
	copyFixtureDir(t, dstDir, "plan_policy_compile")
}

func copyRiskCategoriesFixture(t *testing.T, dstDir string) {
	t.Helper()
	copyFixtureDir(t, dstDir, "plan_risk_categories")
}

func TestPlan_json_includesResolvedConfigDigest(t *testing.T) {
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "plan-json.db")

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
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
	d, ok := payload["resolvedConfigDigest"].(string)
	if !ok || strings.TrimSpace(d) == "" {
		t.Fatalf("resolvedConfigDigest missing or empty: %#v", payload["resolvedConfigDigest"])
	}
}

func TestPlan_json_includesPolicyDigest(t *testing.T) {
	root := t.TempDir()
	copyPolicyCompileFixture(t, root)
	db := filepath.Join(t.TempDir(), "plan-policy-json.db")

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
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
	d, ok := payload["policyDigest"].(string)
	if !ok || strings.TrimSpace(d) == "" {
		t.Fatalf("policyDigest missing or empty: %#v", payload["policyDigest"])
	}
	effective, ok := payload["effectivePolicy"].([]any)
	if !ok || len(effective) < 3 {
		t.Fatalf("effectivePolicy missing entries: %#v", payload["effectivePolicy"])
	}
}

func TestPlan_firstPlan_allCreates(t *testing.T) {
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "plan1.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "Plan: 3 to add, 0 to change, 0 to delete") {
		t.Fatalf("summary missing in:\n%s", s)
	}
	if !strings.HasSuffix(s, "\n") {
		t.Fatalf("expected trailing newline in:\n%s", s)
	}
	for _, line := range []string{"+ create Project/plan-fixture", "+ create Policy/default", "+ create Tool/helper"} {
		if !strings.Contains(s, line) {
			t.Fatalf("missing %q in:\n%s", line, s)
		}
	}
}

func TestPlan_afterApply_noChanges(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "plan2.db")

	g := &Global{ProjectRoot: root}
	graph, _, err := prepareProjectGraph(g)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	pl, err := plan.NewPlanner(st).ComputePlan(ctx, "local", graph)
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
	s := out.String()
	if !strings.Contains(s, "Plan: 0 to add, 0 to change, 0 to delete") {
		t.Fatalf("expected empty plan:\n%s", s)
	}
}

func TestPlan_policyCostIncrease_riskDelta(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "plan3.db")

	g := &Global{ProjectRoot: root}
	graph, _, err := prepareProjectGraph(g)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	pl0, err := plan.NewPlanner(st).ComputePlan(ctx, "local", graph)
	if err != nil {
		t.Fatal(err)
	}
	if err := apply.NewApplier(st).ApplyPlan(ctx, "local", graph, pl0, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	policyPath := filepath.Join(root, "policy.yaml")
	b, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(b), "maxTotalCostUsd: 3", "maxTotalCostUsd: 10", 1)
	if err := os.WriteFile(policyPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "~ update Policy/default") {
		t.Fatalf("expected policy update in:\n%s", s)
	}
	if !strings.Contains(s, "maxTotalCostUsd") {
		t.Fatalf("expected field diff in:\n%s", s)
	}
	if !strings.Contains(s, "Cost ceiling increased") {
		t.Fatalf("expected risk line in:\n%s", s)
	}
	if !strings.Contains(s, "[high] budget_relaxation:") {
		t.Fatalf("expected labeled budget_relaxation item in:\n%s", s)
	}
	if !strings.Contains(s, "high:\n") {
		t.Fatalf("expected high severity group in:\n%s", s)
	}
}

func applyProjectGraph(t *testing.T, root, db string) {
	t.Helper()
	ctx := context.Background()
	g := &Global{ProjectRoot: root}
	graph, _, err := prepareProjectGraph(g)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	pl, err := plan.NewPlanner(st).ComputePlan(ctx, "local", graph)
	if err != nil {
		t.Fatal(err)
	}
	if err := apply.NewApplier(st).ApplyPlan(ctx, "local", graph, pl, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func mutateRiskCategories(t *testing.T, root string) {
	t.Helper()
	replaceFile(t, filepath.Join(root, "policy.yaml"), "maxTotalCostUsd: 3", "maxTotalCostUsd: 10")
	replaceFile(t, filepath.Join(root, "policy.yaml"), "maxWallClockSeconds: 60", "maxWallClockSeconds: 120")
	replaceFile(t, filepath.Join(root, "policy.yaml"), "      - tool.helper.echo\n", "")
	replaceFile(t, filepath.Join(root, "agent.yaml"), "  model: mock/gpt-4\n", "  model: mock/gpt-4o\n")
	replaceFile(t, filepath.Join(root, "agent.yaml"), "    - helper\n", "    - helper\n    - github\n")
	replaceFile(t, filepath.Join(root, "github.yaml"), "      - contents.read\n", "      - contents.read\n      - issues.write\n")
}

func replaceFile(t *testing.T, path, old, new string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	// Git checkout on Windows may yield CRLF fixtures; needles in tests are LF-only.
	if strings.Contains(body, "\r\n") {
		old = strings.ReplaceAll(old, "\n", "\r\n")
		new = strings.ReplaceAll(new, "\n", "\r\n")
	}
	updated := strings.Replace(body, old, new, 1)
	if updated == body {
		t.Fatalf("replace %q not found in %s", old, path)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceFile_crlfNewlines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	crlf := "approvals:\r\n  requiredFor:\r\n      - tool.helper.echo\r\n      - tool.github.issues.write\r\n"
	if err := os.WriteFile(path, []byte(crlf), 0o644); err != nil {
		t.Fatal(err)
	}
	replaceFile(t, path, "      - tool.helper.echo\n", "")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "approvals:\r\n  requiredFor:\r\n      - tool.github.issues.write\r\n"
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPlan_json_riskItems_structuredAndStringList(t *testing.T) {
	root := t.TempDir()
	copyRiskCategoriesFixture(t, root)
	db := filepath.Join(t.TempDir(), "plan-risk-json.db")
	applyProjectGraph(t, root, db)
	mutateRiskCategories(t, root)

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
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
	risk, ok := payload["risk"].([]any)
	if !ok || len(risk) == 0 {
		t.Fatalf("risk string list missing: %#v", payload["risk"])
	}
	items, ok := payload["riskItems"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("riskItems missing: %#v", payload["riskItems"])
	}
	cats := map[string]int{}
	for _, raw := range items {
		m, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("riskItem not object: %#v", raw)
		}
		cat, _ := m["category"].(string)
		cats[cat]++
		if _, ok := m["severity"].(string); !ok {
			t.Fatalf("missing severity: %#v", m)
		}
		if _, ok := m["reason"].(string); !ok {
			t.Fatalf("missing reason: %#v", m)
		}
		if _, ok := m["target"].(map[string]any); !ok {
			t.Fatalf("missing target: %#v", m)
		}
		if hops, ok := m["witness"].([]any); ok && len(hops) > 0 {
			hop, _ := hops[0].(map[string]any)
			if hop["kind"] == nil || hop["reachability"] == nil {
				t.Fatalf("witness hop missing kind/reachability: %#v", hop)
			}
		}
	}
	for _, want := range []string{
		"approval_removal",
		"budget_relaxation",
		"model_change",
		"permission_widening",
		"tool_surface_change",
	} {
		if cats[want] == 0 {
			t.Fatalf("missing category %s in %#v\nbody=%s", want, cats, out.String())
		}
	}
	if cats["approval_removal"] < 1 || cats["budget_relaxation"] < 1 {
		t.Fatalf("combined approval+budget not distinct: %#v", cats)
	}
}

func TestPlan_yaml_riskItems_structuredAndStringList(t *testing.T) {
	root := t.TempDir()
	copyRiskCategoriesFixture(t, root)
	db := filepath.Join(t.TempDir(), "plan-risk-yaml.db")
	applyProjectGraph(t, root, db)
	mutateRiskCategories(t, root)

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
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
	if len(payload.Risk) == 0 {
		t.Fatalf("risk string list missing:\n%s", out.String())
	}
	if len(payload.RiskItems) == 0 {
		t.Fatalf("riskItems missing:\n%s", out.String())
	}
	var sawWitness bool
	cats := map[plan.RiskCategory]int{}
	for _, it := range payload.RiskItems {
		cats[it.Category]++
		if len(it.Witness) > 0 && it.Witness[0].Kind != "" && it.Witness[0].Reachability != "" {
			sawWitness = true
		}
	}
	if !sawWitness {
		t.Fatalf("yaml riskItems missing typed witness hops:\n%s", out.String())
	}
	if cats[plan.RiskCategoryApprovalRemoval] < 1 || cats[plan.RiskCategoryBudgetRelaxation] < 1 {
		t.Fatalf("yaml combined approval+budget not distinct: %#v", cats)
	}
}
