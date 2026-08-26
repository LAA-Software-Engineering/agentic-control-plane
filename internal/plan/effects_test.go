package plan

import (
	"context"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func githubOpsTool() *spec.ToolResource {
	return &spec.ToolResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindTool,
		Metadata:   spec.Metadata{Name: "github"},
		Spec: spec.ToolSpec{
			Type: "native",
			Operations: map[string]spec.ToolOperation{
				"read_pr":      {Effects: []string{"github.read"}},
				"post_comment": {Effects: []string{"github.write", "external.visible"}},
				"merge_pr":     {Effects: []string{"github.write", "destructive"}},
			},
		},
	}
}

func effectGraph(agentTools []string, steps []spec.WorkflowStep) *spec.ProjectGraph {
	g := minimalGraph()
	g.Tools["github"] = githubOpsTool()
	g.Agents["reviewer"] = &spec.AgentResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindAgent,
		Metadata:   spec.Metadata{Name: "reviewer"},
		Spec:       spec.AgentSpec{Model: "mock/gpt-4", Tools: agentTools},
	}
	g.Workflows["pr-review"] = &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindWorkflow,
		Metadata:   spec.Metadata{Name: "pr-review"},
		Spec:       spec.WorkflowSpec{Steps: steps},
	}
	g.Policies["default"] = &spec.PolicyResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindPolicy,
		Metadata:   spec.Metadata{Name: "default"},
		Spec: spec.PolicySpec{
			Effects: &spec.PolicyEffects{
				Permit: []string{"github.read", "github.write", "external.visible"},
			},
		},
	}
	return g
}

func TestEffectBound_firstApply_emptyBaselineTreatsEffectsAsNew(t *testing.T) {
	g := effectGraph([]string{"tool.github.post_comment"}, []spec.WorkflowStep{
		{ID: "fetch_pr", Uses: "tool.github.read_pr"},
		{ID: "review", Agent: "reviewer"},
	})
	pl, err := NewPlanner(&fakeDeploy{list: nil}).ComputePlan(context.Background(), "dev", g)
	if err != nil {
		t.Fatal(err)
	}
	if !pl.Authority.EmptyBaseline {
		t.Fatal("empty store must set emptyBaseline")
	}
	if pl.Authority.Autonomous != AuthorityWidened {
		t.Fatalf("autonomous: %s", pl.Authority.Autonomous)
	}
	if !hasBoundIdent(pl, "github.read") || !hasBoundIdent(pl, "github.write") {
		t.Fatalf("bound missing idents: %#v", pl.EffectBound)
	}
	if !hasBoundUnreachable(pl, "destructive") {
		t.Fatalf("merge_pr destructive should be unreachable: %#v", pl.EffectBound)
	}
	if !hasRiskCategory(pl, RiskCategoryEffectDelta) || !hasRiskCategory(pl, RiskCategoryCapabilityDelta) {
		t.Fatalf("empty baseline should report new effects and capabilities: %#v", pl.Risk.Items)
	}
	got := FormatPlan(pl)
	if !strings.Contains(got, "Effect bound (Workflow/pr-review):") {
		t.Fatalf("missing workflow bound:\n%s", got)
	}
	if !strings.Contains(got, "autonomous  -> WIDENED") {
		t.Fatalf("missing AUTONOMOUS WIDENED:\n%s", got)
	}
}

func TestEffectDelta_newAutonomousGrant(t *testing.T) {
	oldG := effectGraph(nil, []spec.WorkflowStep{
		{ID: "fetch_pr", Uses: "tool.github.read_pr"},
	})
	applied := appliedFromDesired(t, "dev", oldG)
	newG := effectGraph([]string{"tool.github.post_comment"}, []spec.WorkflowStep{
		{ID: "fetch_pr", Uses: "tool.github.read_pr"},
		{ID: "review", Agent: "reviewer"},
	})

	pl, err := NewPlanner(&fakeDeploy{list: applied}).ComputePlan(context.Background(), "dev", newG)
	if err != nil {
		t.Fatal(err)
	}
	if pl.Authority.EmptyBaseline {
		t.Fatal("applied store is not an empty baseline")
	}
	if pl.Authority.Autonomous != AuthorityWidened {
		t.Fatalf("autonomous: %s", pl.Authority.Autonomous)
	}
	var sawWrite, sawCap bool
	for _, it := range pl.Risk.Items {
		if it.Category == RiskCategoryEffectDelta && strings.Contains(it.Ident, "github.write") {
			sawWrite = true
			if it.Severity != RiskSeverityHigh {
				t.Fatalf("new autonomous effect must be high: %#v", it)
			}
			if it.Reachability != WitnessAutonomous {
				t.Fatalf("reachability: %#v", it)
			}
			if len(it.Witness) == 0 {
				t.Fatalf("missing witness: %#v", it)
			}
		}
		if it.Category == RiskCategoryCapabilityDelta && it.Ident == "tool.github.post_comment" {
			sawCap = true
			if it.Target.Name != "reviewer" {
				t.Fatalf("capability target: %#v", it)
			}
		}
	}
	if !sawWrite {
		t.Fatalf("expected autonomous github.write effect delta: %#v", pl.Risk.Items)
	}
	if !sawCap {
		t.Fatalf("expected capability + tool.github.post_comment: %#v", pl.Risk.Items)
	}
}

func TestCapabilityDelta_emptyEffectDelta(t *testing.T) {
	// A second tool whose declared effects are already autonomously reachable
	// widens capability with an empty effect-ident set.
	oldG := effectGraph([]string{"tool.github.post_comment"}, []spec.WorkflowStep{
		{ID: "review", Agent: "reviewer"},
	})
	addChatTool(oldG)
	applied := appliedFromDesired(t, "dev", oldG)
	newG := effectGraph([]string{"tool.github.post_comment", "tool.chat.post"}, []spec.WorkflowStep{
		{ID: "review", Agent: "reviewer"},
	})
	addChatTool(newG)

	pl, err := NewPlanner(&fakeDeploy{list: applied}).ComputePlan(context.Background(), "dev", newG)
	if err != nil {
		t.Fatal(err)
	}
	if hasRiskCategory(pl, RiskCategoryEffectDelta) {
		t.Fatalf("effect delta must be empty when effects were already reachable: %#v", effectDeltaItemsOf(pl))
	}
	if !hasRiskCategory(pl, RiskCategoryCapabilityDelta) {
		t.Fatalf("capability must widen: %#v", pl.Risk.Items)
	}
	var sawChat bool
	for _, it := range pl.Risk.Items {
		if it.Category == RiskCategoryCapabilityDelta && it.Ident == "tool.chat.post" {
			sawChat = true
		}
	}
	if !sawChat {
		t.Fatalf("expected + tool.chat.post: %#v", pl.Risk.Items)
	}
	if pl.Authority.Autonomous != AuthorityWidened {
		t.Fatalf("autonomous must WIDEN when a grant is added: %s", pl.Authority.Autonomous)
	}
	if pl.Authority.Static != AuthorityUnchanged {
		t.Fatalf("static should be unchanged: %s", pl.Authority.Static)
	}
	exp := ExportRisk(pl)
	if exp.Authority == nil || exp.Authority.Autonomous != AuthorityWidened {
		t.Fatalf("JSON authority: %#v", exp.Authority)
	}
	if len(exp.CapabilityDelta) == 0 {
		t.Fatal("JSON capabilityDelta missing")
	}
	if len(exp.EffectDelta) != 0 {
		t.Fatalf("JSON effectDelta should be empty: %#v", exp.EffectDelta)
	}
}

func addChatTool(g *spec.ProjectGraph) {
	g.Tools["chat"] = &spec.ToolResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindTool,
		Metadata:   spec.Metadata{Name: "chat"},
		Spec: spec.ToolSpec{
			Type: "native",
			Operations: map[string]spec.ToolOperation{
				"post": {Effects: []string{"github.write", "external.visible"}},
			},
		},
	}
}

func TestEffectDelta_autonomousHigherSeverityThanStatic(t *testing.T) {
	oldG := effectGraph(nil, nil)
	applied := appliedFromDesired(t, "dev", oldG)
	newG := effectGraph([]string{"tool.github.post_comment"}, []spec.WorkflowStep{
		{ID: "fetch_pr", Uses: "tool.github.read_pr"},
		{ID: "review", Agent: "reviewer"},
	})

	pl, err := NewPlanner(&fakeDeploy{list: applied}).ComputePlan(context.Background(), "dev", newG)
	if err != nil {
		t.Fatal(err)
	}
	var staticSev, autoSev RiskSeverity
	for _, it := range pl.Risk.Items {
		if it.Category != RiskCategoryEffectDelta {
			continue
		}
		switch {
		case strings.Contains(it.Ident, "github.read") && it.Reachability == WitnessStatic:
			staticSev = it.Severity
		case strings.Contains(it.Ident, "github.write") && it.Reachability == WitnessAutonomous:
			autoSev = it.Severity
		}
	}
	if staticSev == "" || autoSev == "" {
		t.Fatalf("need static and autonomous effect deltas: %#v", effectDeltaItemsOf(pl))
	}
	if riskSevRank(autoSev) >= riskSevRank(staticSev) {
		t.Fatalf("autonomous %s must outrank static %s", autoSev, staticSev)
	}
}

func TestEffectDelta_staticToAutonomousPromotion(t *testing.T) {
	oldG := effectGraph(nil, []spec.WorkflowStep{
		{ID: "comment", Uses: "tool.github.post_comment"},
	})
	applied := appliedFromDesired(t, "dev", oldG)
	newG := effectGraph([]string{"tool.github.post_comment"}, []spec.WorkflowStep{
		{ID: "comment", Uses: "tool.github.post_comment"},
		{ID: "review", Agent: "reviewer"},
	})

	pl, err := NewPlanner(&fakeDeploy{list: applied}).ComputePlan(context.Background(), "dev", newG)
	if err != nil {
		t.Fatal(err)
	}
	var promo bool
	for _, it := range pl.Risk.Items {
		if it.Category != RiskCategoryEffectDelta {
			continue
		}
		if strings.Contains(strings.ToLower(it.Reason), "became autonomously reachable") {
			promo = true
			if it.Severity != RiskSeverityHigh {
				t.Fatalf("promotion must be high: %#v", it)
			}
		}
	}
	if !promo {
		t.Fatalf("expected static→autonomous promotion: %#v", effectDeltaItemsOf(pl))
	}
}

func TestGraphFromApplied_roundTrip(t *testing.T) {
	g := effectGraph([]string{"tool.github.post_comment"}, []spec.WorkflowStep{
		{ID: "fetch_pr", Uses: "tool.github.read_pr"},
	})
	applied := appliedFromDesired(t, "dev", g)
	got := graphFromApplied(applied)
	if got.Agents["reviewer"] == nil || got.Tools["github"] == nil || got.Workflows["pr-review"] == nil {
		t.Fatalf("reconstructed graph missing resources: agents=%d tools=%d workflows=%d",
			len(got.Agents), len(got.Tools), len(got.Workflows))
	}
	if got.Agents["reviewer"].Spec.Tools[0] != "tool.github.post_comment" {
		t.Fatalf("tools: %#v", got.Agents["reviewer"].Spec.Tools)
	}
}

func TestExportRisk_effectBoundWitnessAndAuthority(t *testing.T) {
	g := effectGraph([]string{"tool.github.post_comment"}, []spec.WorkflowStep{
		{ID: "fetch_pr", Uses: "tool.github.read_pr"},
		{ID: "review", Agent: "reviewer"},
	})
	pl, err := NewPlanner(&fakeDeploy{list: nil}).ComputePlan(context.Background(), "dev", g)
	if err != nil {
		t.Fatal(err)
	}
	exp := ExportRisk(pl)
	if len(exp.EffectBound) == 0 {
		t.Fatal("effectBound missing")
	}
	var sawHop bool
	for _, sec := range exp.EffectBound {
		for _, it := range sec.Items {
			if len(it.Witness) > 0 && it.Witness[0].Kind != "" {
				sawHop = true
			}
		}
	}
	if !sawHop {
		t.Fatalf("bound items need structured witness: %#v", exp.EffectBound)
	}
	if exp.Authority == nil || exp.Authority.Autonomous != AuthorityWidened {
		t.Fatalf("authority: %#v", exp.Authority)
	}
}

func TestFormatPlan_capabilityAndAuthoritySections(t *testing.T) {
	oldG := effectGraph([]string{"tool.github.post_comment"}, []spec.WorkflowStep{
		{ID: "review", Agent: "reviewer"},
	})
	addChatTool(oldG)
	applied := appliedFromDesired(t, "dev", oldG)
	newG := effectGraph([]string{"tool.github.post_comment", "tool.chat.post"}, []spec.WorkflowStep{
		{ID: "review", Agent: "reviewer"},
	})
	addChatTool(newG)
	pl, err := NewPlanner(&fakeDeploy{list: applied}).ComputePlan(context.Background(), "dev", newG)
	if err != nil {
		t.Fatal(err)
	}
	got := FormatPlan(pl)
	if !strings.Contains(got, "Capability delta:") || !strings.Contains(got, "+ tool.chat.post") {
		t.Fatalf("capability section:\n%s", got)
	}
	if strings.Contains(got, "Effect delta:") {
		t.Fatalf("effect delta should be omitted when empty:\n%s", got)
	}
	if !strings.Contains(got, "autonomous  -> WIDENED") {
		t.Fatalf("authority:\n%s", got)
	}
}

func hasBoundIdent(pl *Plan, ident string) bool {
	for _, sec := range pl.EffectBound {
		for _, it := range sec.Items {
			if it.Ident == ident && it.Reachability != "" {
				return true
			}
		}
	}
	return false
}

func hasBoundUnreachable(pl *Plan, ident string) bool {
	for _, sec := range pl.EffectBound {
		for _, it := range sec.Items {
			if it.Ident == ident && strings.Contains(it.Reason, "unreachable") {
				return true
			}
		}
	}
	return false
}

func effectDeltaItemsOf(pl *Plan) []RiskItem {
	var out []RiskItem
	if pl == nil {
		return out
	}
	for _, it := range pl.Risk.Items {
		if it.Category == RiskCategoryEffectDelta {
			out = append(out, it)
		}
	}
	return out
}
