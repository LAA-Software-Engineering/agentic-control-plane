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

	"github.com/Terfyn/terfyn/internal/apply"
	"github.com/Terfyn/terfyn/internal/plan"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
	"gopkg.in/yaml.v3"
)

// copyFixtureDir copies testdata/<fixtureName> into a subdirectory of dstDir named after the fixture,
// and returns that subdirectory. The subdir name matters: an .agent-only project takes its name from
// its directory basename (issue #430), so copying into a fixture-named subdir (rather than the bare
// temp dir, whose basename is "001") gives the project a stable, meaningful name. Recurses into
// subdirectories so schema/ trees and workflow-test fixtures come along.
func copyFixtureDir(t *testing.T, dstDir, fixtureName string) string {
	t.Helper()
	src := filepath.Join("testdata", fixtureName)
	dst := filepath.Join(dstDir, fixtureName)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	copyTreeInto(t, src, dst)
	return dst
}

func copyTreeInto(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
			copyTreeInto(t, s, d)
			continue
		}
		b, err := os.ReadFile(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(d, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func copyPlanFixture(t *testing.T, dstDir string) string {
	t.Helper()
	return copyFixtureDir(t, dstDir, "plan_project")
}

func copyPolicyCompileFixture(t *testing.T, dstDir string) string {
	t.Helper()
	return copyFixtureDir(t, dstDir, "plan_policy_compile")
}

func copyRiskCategoriesFixture(t *testing.T, dstDir string) string {
	t.Helper()
	return copyFixtureDir(t, dstDir, "plan_risk_categories")
}

func TestPlan_json_includesResolvedConfigDigest(t *testing.T) {
	root := t.TempDir()
	root = copyPlanFixture(t, root)
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
	root = copyPolicyCompileFixture(t, root)
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
	root = copyPlanFixture(t, root)
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
	for _, line := range []string{"+ create Project/plan_project", "+ create Policy/default", "+ create Tool/helper"} {
		if !strings.Contains(s, line) {
			t.Fatalf("missing %q in:\n%s", line, s)
		}
	}
}

func TestPlan_afterApply_noChanges(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	root = copyPlanFixture(t, root)
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
	s := out.String()
	if !strings.Contains(s, "Plan: 0 to add, 0 to change, 0 to delete") {
		t.Fatalf("expected empty plan:\n%s", s)
	}
}

func TestPlan_policyCostIncrease_riskDelta(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	root = copyPlanFixture(t, root)
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
	pl0, err := plan.NewPlanner(st).ComputePlan(ctx, "local", graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := apply.NewApplier(st).ApplyPlan(ctx, "local", graph, pl0, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	policyPath := filepath.Join(root, "main.agent")
	b, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(b), "maxTotalCostUsd 3", "maxTotalCostUsd 10", 1)
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

func copyPlanEffectPermitFixture(t *testing.T, dstDir string) string {
	t.Helper()
	return copyFixtureDir(t, dstDir, "plan_effect_permit")
}

func TestPlan_effectUnpermitted_exit2(t *testing.T) {
	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--project", testdataPath(t, "validate_effect_unpermitted"), "--state", filepath.Join(t.TempDir(), "plan-fx.db")})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d want 2 err=%v\n%s", ExitCodeOf(err), err, out.String())
	}
	if ExitCodeOf(err) == ExitPolicyDenied {
		t.Fatal("must not use exit 5")
	}
	if !strings.Contains(err.Error(), "effect not permitted by policy") {
		t.Fatalf("message: %v", err)
	}
	if !strings.Contains(err.Error(), "AUTONOMOUS") {
		t.Fatalf("AUTONOMOUS: %v", err)
	}
}

func TestPlan_effectPermitWidening_riskItem(t *testing.T) {
	root := t.TempDir()
	root = copyPlanEffectPermitFixture(t, root)
	db := filepath.Join(t.TempDir(), "plan-effect-permit.db")
	applyProjectGraph(t, root, db)
	replaceFile(t, filepath.Join(root, "main.agent"), "permit { github.read }", "permit { github.read github.write }")

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
	items, ok := payload["riskItems"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("riskItems missing: %#v", payload["riskItems"])
	}
	var found bool
	for _, raw := range items {
		m, _ := raw.(map[string]any)
		if m["category"] != "effect_permit_widening" {
			continue
		}
		found = true
		if m["severity"] != "high" {
			t.Fatalf("severity: %#v", m)
		}
		reason, _ := m["reason"].(string)
		if !strings.Contains(reason, "github.write") {
			t.Fatalf("reason: %s", reason)
		}
		if strings.Contains(strings.ToLower(reason), "tight") {
			t.Fatalf("must not call permit widening tightening: %s", reason)
		}
	}
	if !found {
		t.Fatalf("missing effect_permit_widening in %#v\n%s", items, out.String())
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
	pl, err := plan.NewPlanner(st).ComputePlan(ctx, "local", graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := apply.NewApplier(st).ApplyPlan(ctx, "local", graph, pl, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func mutateRiskCategories(t *testing.T, root string) {
	t.Helper()
	p := filepath.Join(root, "main.agent")
	replaceFile(t, p, "maxTotalCostUsd 3", "maxTotalCostUsd 10")
	replaceFile(t, p, "maxWallClockSeconds 60", "maxWallClockSeconds 120")
	// Drop the tool.helper.echo approval requirement (12-space requiredFor entry, distinct from the
	// 8-space agent grant of the same path).
	replaceFile(t, p, "            tool.helper.echo\n", "")
	replaceFile(t, p, "    model mock/gpt-4\n", "    model mock/gpt-4o\n")
	// The agent gains the side-effecting `github` tool — the tool_surface_change signal (ADR 007 step 1
	// replaced the removed permission-widening heuristic).
	replaceFile(t, p, "    grants {\n        tool.helper.echo\n    }", "    grants {\n        tool.helper.echo\n        tool.github.default\n    }")
	// Make github write-capable so the surface change is high (only github declares `trusted true`).
	replaceFile(t, p, "trusted true\n        sideEffects false", "trusted true\n        sideEffects true")
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
	root = copyRiskCategoriesFixture(t, root)
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
	root = copyRiskCategoriesFixture(t, root)
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

func TestPlan_json_effectBoundAndAuthority(t *testing.T) {
	root := t.TempDir()
	root = copyEffectBoundFixture(t, root)
	db := filepath.Join(t.TempDir(), "plan-effect-bound-json.db")

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
	bound, ok := payload["effectBound"].([]any)
	if !ok || len(bound) == 0 {
		t.Fatalf("effectBound missing: %#v", payload["effectBound"])
	}
	auth, ok := payload["authority"].(map[string]any)
	if !ok {
		t.Fatalf("authority missing: %#v", payload["authority"])
	}
	if auth["autonomous"] != "widened" {
		t.Fatalf("authority.autonomous: %#v", auth["autonomous"])
	}
	var sawWitness bool
	for _, raw := range bound {
		sec, _ := raw.(map[string]any)
		items, _ := sec["items"].([]any)
		for _, ir := range items {
			it, _ := ir.(map[string]any)
			if hops, ok := it["witness"].([]any); ok && len(hops) > 0 {
				hop, _ := hops[0].(map[string]any)
				if hop["kind"] != nil && hop["reachability"] != nil {
					sawWitness = true
				}
			}
		}
	}
	if !sawWitness {
		t.Fatalf("effectBound items missing structured witness:\n%s", out.String())
	}
}

func TestPlan_addGrant_autonomousEffectDelta(t *testing.T) {
	root := t.TempDir()
	root = copyEffectBoundFixture(t, root)
	replaceFile(t, filepath.Join(root, "main.agent"), "        tool.github.post_comment\n", "")
	db := filepath.Join(t.TempDir(), "plan-effect-grant.db")
	applyProjectGraph(t, root, db)
	replaceFile(t, filepath.Join(root, "main.agent"), "    grants {\n    }", "    grants {\n        tool.github.post_comment\n    }")

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
	auth, _ := payload["authority"].(map[string]any)
	if auth["autonomous"] != "widened" {
		t.Fatalf("authority.autonomous: %#v\n%s", auth, out.String())
	}
	var sawCap, sawWrite bool
	for _, raw := range asSlice(payload["capabilityDelta"]) {
		m, _ := raw.(map[string]any)
		if m["ident"] == "tool.github.post_comment" {
			sawCap = true
		}
	}
	for _, raw := range asSlice(payload["effectDelta"]) {
		m, _ := raw.(map[string]any)
		ident, _ := m["ident"].(string)
		if strings.Contains(ident, "github.write") || strings.Contains(ident, "external.visible") {
			if m["severity"] != "high" || m["reachability"] != "autonomous" {
				t.Fatalf("new autonomous effect: %#v", m)
			}
			sawWrite = true
		}
	}
	if !sawCap {
		t.Fatalf("capabilityDelta missing post_comment:\n%s", out.String())
	}
	if !sawWrite {
		t.Fatalf("effectDelta missing new autonomous write:\n%s", out.String())
	}
}

func TestPlan_capabilityWidensEmptyEffectDelta(t *testing.T) {
	root := t.TempDir()
	root = copyEffectBoundFixture(t, root)
	// Add a `chat` tool inline (present at baseline). Its post op has the same effects the agent already
	// reaches via github.post_comment, so granting it later widens capability with an EMPTY effect delta.
	chat := `
tool chat {
    type native
    safety {
        trusted true
        sideEffects false
    }
    operations {
        post { effects { github.write external.visible } }
    }
}
`
	agentPath := filepath.Join(root, "main.agent")
	b, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentPath, append(b, []byte(chat)...), 0o644); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(t.TempDir(), "plan-cap-only.db")
	applyProjectGraph(t, root, db)
	replaceFile(t, agentPath, "        tool.github.post_comment\n", "        tool.github.post_comment\n        tool.chat.post\n")

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
	if _, ok := payload["effectDelta"]; ok && len(asSlice(payload["effectDelta"])) > 0 {
		t.Fatalf("effectDelta should be empty: %#v\n%s", payload["effectDelta"], out.String())
	}
	var sawChat bool
	for _, raw := range asSlice(payload["capabilityDelta"]) {
		m, _ := raw.(map[string]any)
		if m["ident"] == "tool.chat.post" {
			sawChat = true
		}
	}
	if !sawChat {
		t.Fatalf("capabilityDelta missing tool.chat.post:\n%s", out.String())
	}
	auth, _ := payload["authority"].(map[string]any)
	if auth["autonomous"] != "widened" {
		t.Fatalf("authority.autonomous: %#v", auth)
	}
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}
