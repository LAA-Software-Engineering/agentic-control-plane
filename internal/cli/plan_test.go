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
}
