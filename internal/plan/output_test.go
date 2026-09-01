package plan

import (
	"strings"
	"testing"
)

func TestFormatPlan_groupsRiskItemsBySeverity(t *testing.T) {
	p := &Plan{
		Risk: RiskSummary{
			Items: []RiskItem{
				{Category: RiskCategoryModelChange, Severity: RiskSeverityMedium, Reason: "model changed", Target: RiskTarget{Kind: RiskTargetAgent, Name: "rev"}},
				{Category: RiskCategoryApprovalRemoval, Severity: RiskSeverityHigh, Reason: "approval removed", Target: RiskTarget{Kind: RiskTargetPolicy, Name: "default"}},
				{Category: RiskCategorySafety, Severity: RiskSeverityLow, Reason: "cost ceiling defined", Target: RiskTarget{Kind: RiskTargetPolicy, Name: "default"}},
			},
		},
	}
	got := FormatPlan(p)
	high := strings.Index(got, "high:\n")
	med := strings.Index(got, "medium:\n")
	low := strings.Index(got, "low:\n")
	if high < 0 || med < 0 || low < 0 {
		t.Fatalf("missing severity group headers:\n%s", got)
	}
	if !(high < med && med < low) {
		t.Fatalf("severity groups not high→medium→low:\n%s", got)
	}
	if !strings.Contains(got, "Risk delta:\n") {
		t.Fatalf("missing Risk delta title:\n%s", got)
	}
	if !strings.Contains(got, "[high] approval_removal:") || !strings.Contains(got, "[medium] model_change:") {
		t.Fatalf("missing labeled items:\n%s", got)
	}
}

func TestFormatPlanSection_effectBoundReuse(t *testing.T) {
	got := FormatPlanSection("Effect bound", []RiskItem{
		{Category: RiskCategoryPermissionWidening, Severity: RiskSeverityHigh, Reason: "write-like", Target: RiskTarget{Kind: RiskTargetTool, Name: "github"}},
	})
	if !strings.HasPrefix(got, "Effect bound:\nhigh:\n- [high] permission_widening:") {
		t.Fatalf("section render:\n%s", got)
	}
}

func TestExportRisk_emptySlicesNotNil(t *testing.T) {
	exp := ExportRisk(nil)
	if exp.Risk == nil || exp.RiskItems == nil {
		t.Fatalf("nil slices: %#v", exp)
	}
	m := map[string]any{}
	AttachRiskExport(m, nil)
	if _, ok := m["risk"].([]string); !ok {
		t.Fatalf("risk %T", m["risk"])
	}
	if _, ok := m["riskItems"].([]RiskItem); !ok {
		t.Fatalf("riskItems %T", m["riskItems"])
	}
	if _, ok := m["effectBound"]; ok {
		t.Fatalf("nil plan should omit effectBound")
	}
	if _, ok := m["authority"]; ok {
		t.Fatalf("nil plan should omit authority")
	}
}

func TestExportRisk_includesWitnessHops(t *testing.T) {
	p := &Plan{
		Risk: RiskSummary{
			Messages: []string{"approval removed"},
			Items: []RiskItem{{
				Category: RiskCategoryApprovalRemoval,
				Severity: RiskSeverityHigh,
				Reason:   "approval removed",
				Target:   RiskTarget{Kind: RiskTargetPolicy, Name: "default"},
				Witness: []WitnessHop{{
					Kind:         WitnessKindPolicy,
					Name:         "default",
					Reachability: WitnessStatic,
				}},
			}},
		},
	}
	exp := ExportRisk(p)
	if len(exp.RiskItems) != 1 || len(exp.RiskItems[0].Witness) != 1 {
		t.Fatalf("export %#v", exp)
	}
	if exp.RiskItems[0].Witness[0].Kind != WitnessKindPolicy || exp.RiskItems[0].Witness[0].Reachability != WitnessStatic {
		t.Fatalf("witness %#v", exp.RiskItems[0].Witness)
	}
}

func TestFormatPlan_invocationBounds(t *testing.T) {
	p := &Plan{InvocationBounds: []WorkflowInvocationBounds{
		{Workflow: "ImplementAndReview", Bounds: []InvocationItem{
			{Kind: "agent", Callee: "Implementer", Max: 3},
			{Kind: "agent", Callee: "Reviewer", Max: 3},
			{Kind: "tool", Callee: "tool.t.op", Max: 1000, DataBounded: true},
		}},
	}}
	got := FormatPlan(p)
	if !strings.Contains(got, "Invocation bounds:") {
		t.Fatalf("missing section:\n%s", got)
	}
	if !strings.Contains(got, "agent Implementer: ≤ 3 per run") {
		t.Fatalf("missing Implementer bound:\n%s", got)
	}
	if !strings.Contains(got, "runtime data") {
		t.Fatalf("data-bounded note missing:\n%s", got)
	}
	exp := ExportRisk(p)
	if len(exp.InvocationBounds) != 1 || exp.InvocationBounds[0].Workflow != "ImplementAndReview" {
		t.Fatalf("export missing invocation bounds: %+v", exp.InvocationBounds)
	}
}
