package plan

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/spec"
)

func TestResolveWorkflowRuntime(t *testing.T) {
	g := minimalGraph()
	// Unset → built-in local.
	if got := resolveWorkflowRuntime(g, &spec.WorkflowResource{}); got != "local" {
		t.Fatalf("unset runtime = %q, want local", got)
	}
	// Explicit workflow runtime wins.
	wf := &spec.WorkflowResource{Spec: spec.WorkflowSpec{Runtime: "claude-code"}}
	if got := resolveWorkflowRuntime(g, wf); got != "claude-code" {
		t.Fatalf("explicit runtime = %q", got)
	}
	// Project default applies when the workflow is unset.
	g.Spec.Defaults = &spec.ProjectDefaults{Runtime: "claude-code"}
	if got := resolveWorkflowRuntime(g, &spec.WorkflowResource{}); got != "claude-code" {
		t.Fatalf("default runtime = %q", got)
	}
}

func planFor(t *testing.T, g *spec.ProjectGraph) *Plan {
	t.Helper()
	pl, err := NewPlanner(&fakeDeploy{list: nil}).ComputePlan(context.Background(), "dev", g, nil)
	if err != nil {
		t.Fatal(err)
	}
	return pl
}

// "Done when": plan output for the same program differs only by the runtime-target line across
// runtimes; the effect bound / authority is byte-identical. The runtime is replaceable; the
// authority is not.
func TestPlan_RuntimeIndependentEffectBound(t *testing.T) {
	steps := []spec.WorkflowStep{
		{ID: "fetch_pr", Uses: "tool.github.read_pr"},
		{ID: "review", Agent: "reviewer"},
	}
	gLocal := effectGraph([]string{"tool.github.post_comment"}, steps) // workflow runtime unset → local
	gClaude := effectGraph([]string{"tool.github.post_comment"}, steps)
	gClaude.Workflows["pr-review"].Spec.Runtime = "claude-code"

	pLocal, pClaude := planFor(t, gLocal), planFor(t, gClaude)

	// The effect bound and authority are computed from the graph alone — byte-identical.
	if a, b := mustJSON(t, pLocal.EffectBound), mustJSON(t, pClaude.EffectBound); a != b {
		t.Fatalf("effect bound must be runtime-independent:\n local=%s\n claude=%s", a, b)
	}
	if mustJSON(t, pLocal.Authority) != mustJSON(t, pClaude.Authority) {
		t.Fatalf("authority must be runtime-independent")
	}
	// The runtime targets differ, and that is the only difference.
	if mustJSON(t, pLocal.RuntimeTargets) == mustJSON(t, pClaude.RuntimeTargets) {
		t.Fatal("runtime targets should differ across runtimes")
	}
	rLocal := FormatPlan(pLocal)
	rClaude := FormatPlan(pClaude)
	if rLocal == rClaude {
		t.Fatal("rendered plans should differ (the runtime-target line)")
	}
	// Neither run adds a risk finding for the selection, so replacing the runtime name makes the
	// two renders identical: they differ *only* by the runtime-target line.
	if strings.ReplaceAll(rClaude, "claude-code", "local") != rLocal {
		t.Fatalf("plans differ by more than the runtime-target line:\n--- local ---\n%s\n--- claude ---\n%s", rLocal, rClaude)
	}
	// And the selection is actually surfaced.
	if !strings.Contains(rLocal, "Runtime targets:") || !strings.Contains(rClaude, "Workflow/pr-review -> claude-code") {
		t.Fatalf("runtime target not surfaced:\n%s", rClaude)
	}
}

func TestSummarizeWorkflowRisk_RuntimeChangeEmitsItem(t *testing.T) {
	sink := newRiskSink()
	op := Operation{Action: ActionUpdate, Target: spec.ResourceID{Kind: spec.KindWorkflow, Name: "pr-review"}}
	summarizeWorkflowRisk(sink, op, `{"spec":{"runtime":"local"}}`, `{"spec":{"runtime":"claude-code"}}`, true, "", "")
	items := finalizeRiskItems(sink.items).Items
	if len(items) != 1 || items[0].Category != RiskCategoryRuntimeTargetChange {
		t.Fatalf("expected one runtime_target_change item, got %+v", items)
	}
	if items[0].Target.Kind != RiskTargetWorkflow {
		t.Fatalf("target = %+v", items[0].Target)
	}
}

func TestSummarizeWorkflowRisk_NoChangeNoItem(t *testing.T) {
	sink := newRiskSink()
	op := Operation{Action: ActionUpdate, Target: spec.ResourceID{Kind: spec.KindWorkflow, Name: "pr-review"}}
	// Unset vs explicit "local" resolve to the same effective target — not a change.
	summarizeWorkflowRisk(sink, op, `{"spec":{}}`, `{"spec":{"runtime":"local"}}`, true, "", "")
	// A create emits nothing (selection is shown by the Runtime targets section, not a risk item).
	summarizeWorkflowRisk(sink,
		Operation{Action: ActionCreate, Target: spec.ResourceID{Kind: spec.KindWorkflow, Name: "pr-review"}},
		"", `{"spec":{"runtime":"claude-code"}}`, false, "", "")
	if items := finalizeRiskItems(sink.items).Items; len(items) != 0 {
		t.Fatalf("expected no risk items, got %+v", items)
	}
}

// A workflow that leaves spec.runtime unset while the project default flips emits NO per-workflow
// item (its own field is byte-unchanged) — the move is reported once at project scope instead.
func TestSummarizeWorkflowRisk_DefaultFlipNotDoubleCounted(t *testing.T) {
	sink := newRiskSink()
	op := Operation{Action: ActionUpdate, Target: spec.ResourceID{Kind: spec.KindWorkflow, Name: "pr-review"}}
	summarizeWorkflowRisk(sink, op, `{"spec":{}}`, `{"spec":{}}`, true, "local", "claude-code")
	if items := finalizeRiskItems(sink.items).Items; len(items) != 0 {
		t.Fatalf("unset-in-both workflow must not emit on a default flip, got %+v", items)
	}
}

// The default flip itself is surfaced once at project scope.
func TestSummarizeProjectRuntimeRisk(t *testing.T) {
	sink := newRiskSink()
	summarizeProjectRuntimeRisk(sink, "acme", "local", "claude-code")
	items := finalizeRiskItems(sink.items).Items
	if len(items) != 1 || items[0].Category != RiskCategoryRuntimeTargetChange || items[0].Target.Kind != RiskTargetProject {
		t.Fatalf("expected one project-scope runtime_target_change, got %+v", items)
	}
	// An effective no-op default change ("" default == local) emits nothing.
	sink2 := newRiskSink()
	summarizeProjectRuntimeRisk(sink2, "acme", "", "local")
	if items := finalizeRiskItems(sink2.items).Items; len(items) != 0 {
		t.Fatalf("effective no-op default change must not emit, got %+v", items)
	}
}

// End-to-end through ComputePlan: flipping the project default from local to claude-code on a
// prior deployment surfaces exactly one project-scope runtime_target_change (not one per workflow).
func TestComputePlan_ProjectDefaultFlipSurfacedOnce(t *testing.T) {
	steps := []spec.WorkflowStep{{ID: "review", Agent: "reviewer"}}
	prior := effectGraph([]string{"tool.github.post_comment"}, steps) // default unset → local
	current := effectGraph([]string{"tool.github.post_comment"}, steps)
	current.Spec.Defaults = &spec.ProjectDefaults{Runtime: "claude-code"}

	applied := appliedFromDesired(t, "dev", prior)
	pl, err := NewPlanner(&fakeDeploy{list: applied}).ComputePlan(context.Background(), "dev", current, nil)
	if err != nil {
		t.Fatal(err)
	}
	var rt []RiskItem
	for _, it := range pl.Risk.Items {
		if it.Category == RiskCategoryRuntimeTargetChange {
			rt = append(rt, it)
		}
	}
	if len(rt) != 1 || rt[0].Target.Kind != RiskTargetProject {
		t.Fatalf("a default flip should surface once at project scope, got %+v", rt)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
