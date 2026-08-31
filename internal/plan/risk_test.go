package plan

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

func TestActionSuggestsWriteSideEffects(t *testing.T) {
	tests := []struct {
		action string
		want   bool
	}{
		{"issues.write", true},
		{"pull_requests.read", false},
		{"tool.github.pull_request.merge", true},
		{"tool.slack.message.send", true},
		{"contents.read", false},
	}
	for _, tt := range tests {
		if got := ActionSuggestsWriteSideEffects(tt.action); got != tt.want {
			t.Errorf("%q: got %v want %v", tt.action, got, tt.want)
		}
	}
}

func graphWithPolicy(cost float64) *spec.ProjectGraph {
	return graphWithPolicyBudget(cost, 0, nil)
}

func graphWithPolicyBudget(cost float64, wall int, requiredFor []string) *spec.ProjectGraph {
	g := minimalGraph()
	ps := spec.PolicySpec{}
	if cost > 0 || wall > 0 {
		ps.Execution = &spec.PolicyExecution{MaxTotalCostUsd: cost, MaxWallClockSeconds: wall}
	}
	if len(requiredFor) > 0 {
		ps.Approvals = &spec.PolicyApprovals{RequiredFor: requiredFor}
	}
	g.Policies["default"] = &spec.PolicyResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindPolicy,
		Metadata:   spec.Metadata{Name: "default"},
		Spec:       ps,
	}
	return g
}

func graphWithTool(allow []string) *spec.ProjectGraph {
	g := minimalGraph()
	g.Tools["github"] = &spec.ToolResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindTool,
		Metadata:   spec.Metadata{Name: "github"},
		Spec: spec.ToolSpec{
			Type: "mcp",
			Permissions: &spec.ToolPermissions{
				Allow: allow,
			},
		},
	}
	return g
}

func TestRiskSummary_costCeilingIncreased(t *testing.T) {
	oldG := graphWithPolicy(3.0)
	applied := appliedFromDesired(t, "dev", oldG)
	newG := graphWithPolicy(10.0)

	p := NewPlanner(&fakeDeploy{list: applied})
	pl, err := p.ComputePlan(context.Background(), "dev", newG, nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(pl.Risk.Messages, " ")
	if !strings.Contains(strings.ToLower(joined), "cost ceiling increased") {
		t.Fatalf("expected cost ceiling risk, got %#v", pl.Risk.Messages)
	}
}

func TestRiskSummary_newToolCreate_flagsWriteLikeWhenNoPriorState(t *testing.T) {
	g := graphWithTool([]string{"issues.write"})
	p := NewPlanner(&fakeDeploy{list: nil})
	pl, err := p.ComputePlan(context.Background(), "dev", g, nil)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range pl.Risk.Messages {
		if strings.Contains(strings.ToLower(m), "write") && strings.Contains(strings.ToLower(m), "permission") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected baseline tool permission risk, got %#v", pl.Risk.Messages)
	}
}

func TestRiskSummary_effectPermitWidening(t *testing.T) {
	oldG := graphWithPolicyBudget(3, 0, nil)
	oldG.Policies["default"].Spec.Effects = &spec.PolicyEffects{Permit: []string{"github.read"}}
	applied := appliedFromDesired(t, "dev", oldG)
	newG := graphWithPolicyBudget(3, 0, nil)
	newG.Policies["default"].Spec.Effects = &spec.PolicyEffects{Permit: []string{"github.read", "github.write"}}

	p := NewPlanner(&fakeDeploy{list: applied})
	pl, err := p.ComputePlan(context.Background(), "dev", newG, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRiskCategory(pl, RiskCategoryEffectPermitWidening) {
		t.Fatalf("expected effect_permit_widening, got %#v", pl.Risk.Items)
	}
	var n int
	for _, it := range pl.Risk.Items {
		if it.Category != RiskCategoryEffectPermitWidening {
			continue
		}
		n++
		if it.Severity != RiskSeverityHigh {
			t.Fatalf("severity %s", it.Severity)
		}
		if !strings.Contains(it.Reason, "github.write") {
			t.Fatalf("reason should name new ident: %#v", it)
		}
		if strings.Contains(strings.ToLower(it.Reason), "tight") || it.Category == RiskCategoryBudgetRelaxation {
			t.Fatalf("permit widening must not be labeled tightening/budget: %#v", it)
		}
		if len(it.Witness) == 0 || it.Witness[0].Kind != WitnessKindPolicy {
			t.Fatalf("witness: %#v", it.Witness)
		}
	}
	if n != 1 {
		t.Fatalf("want 1 widening item, got %d: %#v", n, pl.Risk.Items)
	}

	covered := graphWithPolicyBudget(3, 0, nil)
	covered.Policies["default"].Spec.Effects = &spec.PolicyEffects{Permit: []string{"github"}}
	applied2 := appliedFromDesired(t, "dev", covered)
	child := graphWithPolicyBudget(3, 0, nil)
	child.Policies["default"].Spec.Effects = &spec.PolicyEffects{Permit: []string{"github", "github.read"}}
	pl2, err := NewPlanner(&fakeDeploy{list: applied2}).ComputePlan(context.Background(), "dev", child, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hasRiskCategory(pl2, RiskCategoryEffectPermitWidening) {
		t.Fatalf("github already covers github.read: %#v", pl2.Risk.Items)
	}
}

func TestRiskSummary_effectPermitWidening_unattendedPromotion(t *testing.T) {
	oldG := graphWithPolicyBudget(3, 0, nil)
	oldG.Policies["default"].Spec.Effects = &spec.PolicyEffects{PermitWithApproval: []string{"github.write"}}
	applied := appliedFromDesired(t, "dev", oldG)
	newG := graphWithPolicyBudget(3, 0, nil)
	newG.Policies["default"].Spec.Effects = &spec.PolicyEffects{Permit: []string{"github.write"}}

	p := NewPlanner(&fakeDeploy{list: applied})
	pl, err := p.ComputePlan(context.Background(), "dev", newG, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRiskCategory(pl, RiskCategoryEffectPermitWidening) {
		t.Fatalf("promoting permitWithApproval to unattended permit must be effect_permit_widening, got %#v", pl.Risk.Items)
	}
	var n int
	for _, it := range pl.Risk.Items {
		if it.Category != RiskCategoryEffectPermitWidening {
			continue
		}
		n++
		if !strings.Contains(it.Reason, "github.write") {
			t.Fatalf("reason should name promoted ident: %#v", it)
		}
	}
	if n != 1 {
		t.Fatalf("want 1 widening item, got %d: %#v", n, pl.Risk.Items)
	}

	dual := graphWithPolicyBudget(3, 0, nil)
	dual.Policies["default"].Spec.Effects = &spec.PolicyEffects{
		Permit:             []string{"github.write"},
		PermitWithApproval: []string{"github.write"},
	}
	plDual, err := NewPlanner(&fakeDeploy{list: applied}).ComputePlan(context.Background(), "dev", dual, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hasRiskCategory(plDual, RiskCategoryEffectPermitWidening) {
		t.Fatalf("dual-list stays approval-gated, not unattended widening: %#v", plDual.Risk.Items)
	}
}

func TestRiskSummary_newWriteLikeToolPermissions(t *testing.T) {
	oldG := graphWithTool([]string{"contents.read"})
	applied := appliedFromDesired(t, "dev", oldG)
	newG := graphWithTool([]string{"contents.read", "issues.write"})

	p := NewPlanner(&fakeDeploy{list: applied})
	pl, err := p.ComputePlan(context.Background(), "dev", newG, nil)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range pl.Risk.Messages {
		if strings.Contains(strings.ToLower(m), "write") && strings.Contains(strings.ToLower(m), "permission") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected write-permission risk, got %#v", pl.Risk.Messages)
	}
	if !hasRiskCategory(pl, RiskCategoryPermissionWidening) {
		t.Fatalf("expected permission_widening item, got %#v", pl.Risk.Items)
	}
}

func TestRiskSummary_approvalRemovalAndCostIncrease_distinctItems(t *testing.T) {
	oldG := graphWithPolicyBudget(3, 60, []string{"tool.helper.echo", "tool.github.issues.write"})
	applied := appliedFromDesired(t, "dev", oldG)
	newG := graphWithPolicyBudget(10, 60, []string{"tool.github.issues.write"})

	p := NewPlanner(&fakeDeploy{list: applied})
	pl, err := p.ComputePlan(context.Background(), "dev", newG, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRiskCategory(pl, RiskCategoryApprovalRemoval) || !hasRiskCategory(pl, RiskCategoryBudgetRelaxation) {
		t.Fatalf("expected approval_removal and budget_relaxation, got %#v", pl.Risk.Items)
	}
	var approval, budget int
	for _, it := range pl.Risk.Items {
		switch it.Category {
		case RiskCategoryApprovalRemoval:
			approval++
			if it.Severity != RiskSeverityHigh {
				t.Fatalf("approval_removal severity %s", it.Severity)
			}
			if !strings.Contains(it.Reason, "tool.helper.echo") {
				t.Fatalf("approval item should name removed action: %#v", it)
			}
			if got := FormatRiskItem(it); !strings.Contains(got, "approval_removal") || !strings.Contains(got, "[high]") {
				t.Fatalf("unlabeled item: %s", got)
			}
		case RiskCategoryBudgetRelaxation:
			budget++
			if it.Severity != RiskSeverityHigh {
				t.Fatalf("budget_relaxation severity %s", it.Severity)
			}
			if !strings.Contains(strings.ToLower(it.Reason), "cost ceiling increased") {
				t.Fatalf("budget item: %#v", it)
			}
		}
	}
	if approval != 1 || budget != 1 {
		t.Fatalf("want 1 approval + 1 cost budget item, got approval=%d budget=%d items=%#v", approval, budget, pl.Risk.Items)
	}
	labeled := FormatPlan(pl)
	if !strings.Contains(labeled, "high:\n") {
		t.Fatalf("plan output missing high severity group:\n%s", labeled)
	}
	if !strings.Contains(labeled, "[high] approval_removal:") || !strings.Contains(labeled, "[high] budget_relaxation:") {
		t.Fatalf("plan output missing labeled items:\n%s", labeled)
	}
}

func TestRiskSummary_wallClockCeilingIncreased(t *testing.T) {
	oldG := graphWithPolicyBudget(3, 60, nil)
	applied := appliedFromDesired(t, "dev", oldG)
	newG := graphWithPolicyBudget(3, 120, nil)

	pl, err := NewPlanner(&fakeDeploy{list: applied}).ComputePlan(context.Background(), "dev", newG, nil)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, it := range pl.Risk.Items {
		if it.Category == RiskCategoryBudgetRelaxation && strings.Contains(strings.ToLower(it.Reason), "wall-clock") {
			found = true
			if it.Severity != RiskSeverityHigh {
				t.Fatalf("wall-clock severity %s", it.Severity)
			}
		}
	}
	if !found {
		t.Fatalf("expected wall-clock budget_relaxation, got %#v", pl.Risk.Items)
	}
}

func TestRiskSummary_agentModelChanged(t *testing.T) {
	oldG := graphWithAgent("mock/gpt-4")
	applied := appliedFromDesired(t, "dev", oldG)
	newG := graphWithAgent("mock/gpt-4o")

	pl, err := NewPlanner(&fakeDeploy{list: applied}).ComputePlan(context.Background(), "dev", newG, nil)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, it := range pl.Risk.Items {
		if it.Category == RiskCategoryModelChange {
			found = true
			if it.Severity != RiskSeverityMedium {
				t.Fatalf("model_change severity %s", it.Severity)
			}
			if it.Target.Kind != RiskTargetAgent || it.Target.Name != "rev" {
				t.Fatalf("target %#v", it.Target)
			}
			if len(it.Witness) != 1 || it.Witness[0].Kind != WitnessKindAgent || it.Witness[0].Reachability != WitnessStatic {
				t.Fatalf("witness %#v", it.Witness)
			}
		}
	}
	if !found {
		t.Fatalf("expected model_change, got %#v", pl.Risk.Items)
	}
}

func TestRiskSummary_agentToolsListGained(t *testing.T) {
	oldG := graphWithAgentTools([]string{"helper"}, []string{})
	applied := appliedFromDesired(t, "dev", oldG)
	newG := graphWithAgentTools([]string{"helper"}, []string{"issues.write"})

	pl, err := NewPlanner(&fakeDeploy{list: applied}).ComputePlan(context.Background(), "dev", newG, nil)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, it := range pl.Risk.Items {
		if it.Category == RiskCategoryToolSurfaceChange {
			found = true
			if it.Severity != RiskSeverityHigh {
				t.Fatalf("write-like tool surface should be high, got %s", it.Severity)
			}
			if !strings.Contains(it.Reason, "github") {
				t.Fatalf("reason %#v", it.Reason)
			}
		}
	}
	if !found {
		t.Fatalf("expected tool_surface_change, got %#v", pl.Risk.Items)
	}
}

func TestRiskItem_witnessPathReadyForEffectDelta(t *testing.T) {
	item := RiskItem{
		Category: RiskCategoryPermissionWidening,
		Severity: RiskSeverityHigh,
		Reason:   "effect-delta placeholder",
		Target:   RiskTarget{Kind: RiskTargetTool, Name: "github"},
		Witness: []WitnessHop{
			{Kind: WitnessKindWorkflow, Name: "pr-review", Reachability: WitnessStatic},
			{Kind: WitnessKindStep, Name: "review", ID: "review", Reachability: WitnessStatic},
			{Kind: WitnessKindAgent, Name: "reviewer", Reachability: WitnessAutonomous},
			{Kind: WitnessKindToolOperation, Name: "issues.write", Reachability: WitnessAutonomous},
		},
	}
	b, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var got RiskItem
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Witness) != 4 {
		t.Fatalf("witness hops: %#v", got.Witness)
	}
	if got.Witness[0].Kind != WitnessKindWorkflow || got.Witness[2].Reachability != WitnessAutonomous {
		t.Fatalf("round-trip %#v", got.Witness)
	}
	if got.Witness[3].Kind != WitnessKindToolOperation || got.Witness[3].Name != "issues.write" {
		t.Fatalf("tool_operation hop %#v", got.Witness[3])
	}
}

func graphWithAgentTools(helperAllow, githubAllow []string) *spec.ProjectGraph {
	g := graphWithAgent("mock/gpt-4")
	g.Tools["helper"] = &spec.ToolResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindTool,
		Metadata:   spec.Metadata{Name: "helper"},
		Spec: spec.ToolSpec{
			Type: "native",
			Permissions: &spec.ToolPermissions{
				Allow: helperAllow,
			},
		},
	}
	g.Tools["github"] = &spec.ToolResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindTool,
		Metadata:   spec.Metadata{Name: "github"},
		Spec: spec.ToolSpec{
			Type: "native",
			Permissions: &spec.ToolPermissions{
				Allow: githubAllow,
			},
		},
	}
	if len(githubAllow) > 0 {
		g.Agents["rev"].Spec.Tools = []string{"helper", "github"}
	} else {
		g.Agents["rev"].Spec.Tools = []string{"helper"}
	}
	return g
}

func hasRiskCategory(pl *Plan, cat RiskCategory) bool {
	if pl == nil {
		return false
	}
	for _, it := range pl.Risk.Items {
		if it.Category == cat {
			return true
		}
	}
	return false
}
