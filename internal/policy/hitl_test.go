package policy

import (
	"errors"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestResolveHitlReview_defaults(t *testing.T) {
	t.Helper()
	pol := &spec.PolicySpec{
		Hitl: &spec.HitlPolicy{
			InterruptOn: map[string]spec.HitlInterruptValue{
				"helper": {Enabled: true},
			},
		},
	}
	review, err := ResolveHitlReview(nil, pol, "tool.helper.echo")
	if err != nil {
		t.Fatal(err)
	}
	if len(review.AllowedDecisions) != 2 {
		t.Fatalf("decisions = %v", review.AllowedDecisions)
	}
	if !strings.Contains(review.Description, "tool.helper.echo") {
		t.Fatalf("description = %q", review.Description)
	}
}

func TestResolveHitlReview_templateAndSwitch(t *testing.T) {
	t.Helper()
	pol := &spec.PolicySpec{
		Hitl: &spec.HitlPolicy{
			ToolSwitchMap: map[string][]string{
				"deploy_to_production": {"deploy_to_staging"},
			},
			InterruptOn: map[string]spec.HitlInterruptValue{
				"deploy": {
					Enabled: true,
					Config: &spec.HitlInterruptConfig{
						Description:      "Deploy ${operation} via ${tool}",
						AllowedDecisions: []spec.HitlDecisionKind{spec.HitlDecisionApprove, spec.HitlDecisionSwitch},
						SwitchMap: map[string][]string{
							"deploy_to_production": {"canary_deploy"},
						},
					},
				},
			},
		},
	}
	review, err := ResolveHitlReview(nil, pol, "tool.deploy.deploy_to_production")
	if err != nil {
		t.Fatal(err)
	}
	if review.Description != "Deploy deploy_to_production via deploy" {
		t.Fatalf("description = %q", review.Description)
	}
	targets := map[string]struct{}{}
	for _, s := range review.SwitchTargets {
		targets[s] = struct{}{}
	}
	for _, want := range []string{"deploy_to_staging", "canary_deploy"} {
		if _, ok := targets[want]; !ok {
			t.Fatalf("missing switch target %q in %v", want, review.SwitchTargets)
		}
	}
}

func TestValidateHitlEdit_denyWins(t *testing.T) {
	t.Helper()
	review := ResolvedHitlReview{
		AllowedEditArgs: []string{"*"},
		DeniedEditArgs:  []string{"secret"},
	}
	orig := map[string]any{"topic": "a", "secret": "x"}
	edited := map[string]any{"topic": "b", "secret": "x"}
	if err := ValidateHitlEdit(orig, edited, review); err != nil {
		t.Fatalf("unchanged secret should pass: %v", err)
	}
	edited["secret"] = "y"
	if err := ValidateHitlEdit(orig, edited, review); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("expected denied path error, got %v", err)
	}
}

func TestValidateHitlEdit_allowedPathOnly(t *testing.T) {
	t.Helper()
	review := ResolvedHitlReview{AllowedEditArgs: []string{"topic"}}
	orig := map[string]any{"topic": "a", "mode": "prod"}
	edited := map[string]any{"topic": "b", "mode": "prod"}
	if err := ValidateHitlEdit(orig, edited, review); err != nil {
		t.Fatal(err)
	}
	edited["mode"] = "staging"
	if err := ValidateHitlEdit(orig, edited, review); err == nil {
		t.Fatal("expected mode change rejection")
	}
}

func TestApplyHitlDecision_branches(t *testing.T) {
	t.Helper()
	gate := HitlGate{
		Uses: "tool.helper.echo",
		With: map[string]any{"topic": "hi"},
		Review: ResolvedHitlReview{
			AllowedDecisions: []spec.HitlDecisionKind{
				spec.HitlDecisionApprove,
				spec.HitlDecisionReject,
				spec.HitlDecisionEdit,
				spec.HitlDecisionSwitch,
			},
			AllowedEditArgs: []string{"topic"},
			SwitchTargets:   []string{"identity"},
		},
	}

	uses, with, err := ApplyHitlDecision(gate, HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"})
	if err != nil || uses != gate.Uses || with["topic"] != "hi" {
		t.Fatalf("approve: uses=%q with=%v err=%v", uses, with, err)
	}

	_, _, err = ApplyHitlDecision(gate, HitlDecisionInput{Kind: spec.HitlDecisionReject, Actor: "alice"})
	if !errors.Is(err, ErrHitlRejected) {
		t.Fatalf("reject: %v", err)
	}

	uses, with, err = ApplyHitlDecision(gate, HitlDecisionInput{
		Kind: spec.HitlDecisionEdit, Actor: "alice", EditedWith: map[string]any{"topic": "bye"},
	})
	if err != nil || with["topic"] != "bye" {
		t.Fatalf("edit: %v uses=%q with=%v", err, uses, with)
	}

	uses, _, err = ApplyHitlDecision(gate, HitlDecisionInput{
		Kind: spec.HitlDecisionSwitch, Actor: "alice", SwitchTarget: "identity",
	})
	if err != nil || uses != "tool.helper.identity" {
		t.Fatalf("switch: uses=%q err=%v", uses, err)
	}
}

func TestApplyHitlDecision_disallowedDecision(t *testing.T) {
	t.Helper()
	gate := HitlGate{
		Uses: "tool.helper.echo",
		Review: ResolvedHitlReview{
			AllowedDecisions: []spec.HitlDecisionKind{spec.HitlDecisionApprove},
		},
	}
	_, _, err := ApplyHitlDecision(gate, HitlDecisionInput{Kind: spec.HitlDecisionEdit})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("got %v", err)
	}
}

func TestToolCallNeedsHitl_preApprovedSkips(t *testing.T) {
	t.Helper()
	graph := testGraphForHitl(t)
	pol := graph.Policies["gate"].Spec
	call := ToolCallContext{
		Run:    RunContext{ApprovedActions: []string{"tool.helper.echo"}},
		Uses:   "tool.helper.echo",
		With:   map[string]any{},
		StepID: "s1",
	}
	need, err := ToolCallNeedsHitl(graph, &pol, call)
	if err != nil || need {
		t.Fatalf("need=%v err=%v", need, err)
	}
}

func TestToolCallNeedsHitl_requiredWithoutApprove(t *testing.T) {
	t.Helper()
	graph := testGraphForHitl(t)
	pol := graph.Policies["gate"].Spec
	call := ToolCallContext{
		Run:    RunContext{},
		Uses:   "tool.helper.echo",
		With:   map[string]any{},
		StepID: "s1",
	}
	need, err := ToolCallNeedsHitl(graph, &pol, call)
	if err != nil || !need {
		t.Fatalf("need=%v err=%v", need, err)
	}
}

func TestRedactHitlArgs_masksKeys(t *testing.T) {
	t.Helper()
	args := map[string]any{"topic": "x", "token": "secret"}
	out := RedactHitlArgs(args, []string{"token"})
	if out["topic"] != "x" {
		t.Fatalf("topic leaked change: %v", out)
	}
	if out["token"] != RedactedSecretPlaceholder {
		t.Fatalf("token not redacted: %v", out["token"])
	}
}

func testGraphForHitl(t *testing.T) *spec.ProjectGraph {
	t.Helper()
	return &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"helper": {
				Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(true)}},
			},
		},
		Policies: map[string]*spec.PolicyResource{
			"gate": {
				Spec: spec.PolicySpec{
					Approvals: &spec.PolicyApprovals{
						RequiredFor: []string{"tool.helper.echo"},
					},
				},
			},
		},
	}
}
