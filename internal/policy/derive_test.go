package policy

import (
	"context"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestDerive_truthTable(t *testing.T) {
	tests := []struct {
		name string
		in   spec.ResolvedToolSafety
		want Decision
	}{
		{"trusted_no_explicit_approval", spec.ResolvedToolSafety{Trusted: true, SideEffects: true, RequiresApproval: false}, DecisionAllow},
		{"untrusted_read_only", spec.ResolvedToolSafety{Trusted: false, SideEffects: false, RequiresApproval: false}, DecisionAllow},
		{"untrusted_mutating", spec.ResolvedToolSafety{Trusted: false, SideEffects: true, RequiresApproval: true}, DecisionRequireApproval},
		{"explicit_requires_approval", spec.ResolvedToolSafety{Trusted: true, SideEffects: false, RequiresApproval: true}, DecisionRequireApproval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Derive(tt.in); got != tt.want {
				t.Fatalf("Derive() = %q want %q", got, tt.want)
			}
		})
	}
}

func TestCheckToolCall_safetyFallback_requiresApprovalWithoutApprove(t *testing.T) {
	g := testGraphWithTools("slack")
	g.Tools["slack"].Spec.Safety = &spec.ToolSafety{Trusted: spec.BoolPtr(false), SideEffects: spec.BoolPtr(true)}
	ev := NewEvaluator(g, nil)
	err := ev.CheckToolCall(context.Background(), ToolCallContext{
		Run:  RunContext{},
		Uses: "tool.slack.message.send",
	})
	if err == nil {
		t.Fatal("expected denial")
	}
	d, ok := AsDenied(err)
	if !ok || d.Reason != ReasonApprovalRequired {
		t.Fatalf("got %v", err)
	}
}

func TestCheckToolCall_safetyFallback_trustedAllows(t *testing.T) {
	g := testGraphWithTools("slack")
	g.Tools["slack"].Spec.Safety = &spec.ToolSafety{Trusted: spec.BoolPtr(true)}
	ev := NewEvaluator(g, nil)
	err := ev.CheckToolCall(context.Background(), ToolCallContext{
		Run:  RunContext{},
		Uses: "tool.slack.message.send",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckToolCall_safetyFallback_approveGrants(t *testing.T) {
	g := testGraphWithTools("slack")
	ev := NewEvaluator(g, nil)
	err := ev.CheckToolCall(context.Background(), ToolCallContext{
		Run:  RunContext{ApprovedActions: []string{"tool.slack.message.send"}},
		Uses: "tool.slack.message.send",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckToolCall_explicitPolicyRuleBeforeSafety(t *testing.T) {
	g := testGraphWithTools("github")
	g.Tools["github"].Spec.Safety = &spec.ToolSafety{Trusted: spec.BoolPtr(true)}
	pol := &spec.PolicySpec{
		Approvals: &spec.PolicyApprovals{
			RequiredFor: []string{"tool.github.pull_request.merge"},
		},
	}
	ev := NewEvaluator(g, pol)
	err := ev.CheckToolCall(context.Background(), ToolCallContext{
		Run:  RunContext{},
		Uses: "tool.github.pull_request.merge",
	})
	if err == nil {
		t.Fatal("expected explicit policy approval denial")
	}
	d, ok := AsDenied(err)
	if !ok || d.Reason != ReasonApprovalRequired {
		t.Fatalf("got %v", err)
	}
	// trusted safety would allow, but explicit rule wins
	err = ev.CheckToolCall(context.Background(), ToolCallContext{
		Run:  RunContext{},
		Uses: "tool.github.pull_request.get",
	})
	if err != nil {
		t.Fatalf("trusted tool without explicit rule should allow: %v", err)
	}
}

func TestApprovalRequired_exactUsesNotPrefix(t *testing.T) {
	g := testGraphWithTools("github")
	g.Tools["github"].Spec.Safety = &spec.ToolSafety{Trusted: spec.BoolPtr(true)}
	pol := &spec.PolicySpec{
		Approvals: &spec.PolicyApprovals{
			RequiredFor: []string{"tool.github.pull_request.merge"},
		},
	}
	ev := NewEvaluator(g, pol)
	if approvalRequired("tool.github.pull_request.get", pol.Approvals) {
		t.Fatal("approvalRequired must not match by prefix")
	}
	if !approvalRequired("tool.github.pull_request.merge", pol.Approvals) {
		t.Fatal("exact uses should require approval")
	}
	td := EffectiveToolDecision(g, pol, "github")
	if td.Source != SourceExplicitPolicyRule {
		t.Fatalf("plan uses prefix conservatively: %+v", td)
	}
	err := ev.CheckToolCall(context.Background(), ToolCallContext{
		Run: RunContext{}, Uses: "tool.github.pull_request.get",
	})
	if err != nil {
		t.Fatalf("trusted + no exact requiredFor: %v", err)
	}
}

func TestEffectiveToolDecision_explicitVsDerived(t *testing.T) {
	g := testGraphWithTools("github")
	g.Tools["github"].Spec.Safety = &spec.ToolSafety{Trusted: spec.BoolPtr(true)}
	pol := &spec.PolicySpec{
		Approvals: &spec.PolicyApprovals{
			RequiredFor: []string{"tool.github.pull_request.merge"},
		},
	}
	td := EffectiveToolDecision(g, pol, "github")
	if td.Decision != DecisionRequireApproval || td.Source != SourceExplicitPolicyRule {
		t.Fatalf("prefix rule: %+v", td)
	}
	td = EffectiveToolDecision(g, nil, "github")
	if td.Decision != DecisionAllow || td.Source != SourceSafetyMetadata {
		t.Fatalf("trusted metadata: %+v", td)
	}
}
