package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/apply"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
)

func TestDiff_firstPlan_threeCreates(t *testing.T) {
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "diff1.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"diff", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "Project/plan-fixture (create)") {
		t.Fatalf("missing project create:\n%s", s)
	}
	if !strings.Contains(s, "Policy/default (create)") || !strings.Contains(s, "Tool/helper (create)") {
		t.Fatalf("missing policy/tool:\n%s", s)
	}
	if !strings.Contains(s, "Desired specification:") {
		t.Fatalf("missing desired block:\n%s", s)
	}
}

func TestDiff_afterApply_noDifferences(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "diff2.db")

	g := &Global{ProjectRoot: root}
	graph, _, err := prepareProjectGraph(root, g)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
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
	cmd.SetArgs([]string{"diff", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No differences between desired configuration and applied state.") {
		t.Fatalf("got:\n%s", out.String())
	}
}

func TestDiff_singleResource_inSync(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "diff3.db")

	g := &Global{ProjectRoot: root}
	graph, _, err := prepareProjectGraph(root, g)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
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
	cmd.SetArgs([]string{"diff", "tool/helper", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "No differences for Tool/helper") {
		t.Fatalf("got:\n%s", s)
	}
}

func TestDiff_singleResource_policyUpdate(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "diff4.db")

	g := &Global{ProjectRoot: root}
	graph, _, err := prepareProjectGraph(root, g)
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
	cmd.SetArgs([]string{"diff", "Policy/default", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "Policy/default (update)") {
		t.Fatalf("got:\n%s", s)
	}
	if !strings.Contains(s, "maxTotalCostUsd") {
		t.Fatalf("expected field path in:\n%s", s)
	}
	if !strings.Contains(s, "Applied specification:") || !strings.Contains(s, "Desired specification:") {
		t.Fatalf("expected side context:\n%s", s)
	}
}

func TestDiff_unknownResource_exit2(t *testing.T) {
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "diff5.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"diff", "Agent/missing", "--project", root, "--state", db})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("code=%d err=%v", ExitCodeOf(err), err)
	}
}

func TestDiff_badKindName_exit2(t *testing.T) {
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "diff6.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"diff", "NotAKind/foo", "--project", root, "--state", db})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("code=%d err=%v", ExitCodeOf(err), err)
	}
}

func TestDiff_tooManyArgs_exit2(t *testing.T) {
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "diff7.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"diff", "Policy/a", "Policy/b", "--project", root, "--state", db})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("code=%d err=%v", ExitCodeOf(err), err)
	}
}

func TestDiff_json_firstPlan(t *testing.T) {
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "diff8.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"diff", "-o", "json", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	sum, ok := m["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary: %v", m["summary"])
	}
	if int(sum["create"].(float64)) != 3 || int(sum["update"].(float64)) != 0 || int(sum["delete"].(float64)) != 0 {
		t.Fatalf("summary: %v", sum)
	}
	res, ok := m["resources"].([]any)
	if !ok || len(res) != 3 {
		t.Fatalf("resources: %v", m["resources"])
	}
}

func TestDiff_json_inSyncSingleTarget(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "diff9.db")

	g := &Global{ProjectRoot: root}
	graph, _, err := prepareProjectGraph(root, g)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
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
	cmd.SetArgs([]string{"diff", "-o", "json", "Project/plan-fixture", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["inSync"] != true {
		t.Fatalf("inSync: %v", m)
	}
	if m["atTarget"] != "Project/plan-fixture" {
		t.Fatalf("atTarget: %v", m)
	}
}
