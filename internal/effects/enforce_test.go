package effects

import (
	"errors"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

func TestCheck_skipsWhenNoDeclaredOperations(t *testing.T) {
	t.Parallel()
	g := graph(
		map[string]*spec.ToolResource{
			"helper": {Metadata: spec.Metadata{Name: "helper"}, Spec: spec.ToolSpec{Type: "native"}},
		},
		nil,
		workflow("demo", stepUses("ping", "tool.helper.echo")),
	)
	g.Policies = map[string]*spec.PolicyResource{
		"default": {Metadata: spec.Metadata{Name: "default"}},
	}
	g.Workflows["demo"].Spec.Policy = "default"
	if err := Check(g); err != nil {
		t.Fatalf("existing tools without operations must skip: %v", err)
	}
}

func TestCheck_unpermittedWorkflowAutonomous(t *testing.T) {
	t.Parallel()
	g := graph(
		map[string]*spec.ToolResource{
			"kubernetes": {
				Metadata: spec.Metadata{Name: "kubernetes"},
				Spec: spec.ToolSpec{
					Type: "native",
					Operations: map[string]spec.ToolOperation{
						"restart": {Effects: []string{"production.write", "destructive"}},
					},
				},
			},
		},
		agent("deploy-agent", "tool.kubernetes.restart"),
		workflow("deploy-production", stepAgent("remediate", "deploy-agent")),
	)
	g.Policies = map[string]*spec.PolicyResource{
		"staging-only": {
			Metadata: spec.Metadata{Name: "staging-only"},
			Spec: spec.PolicySpec{
				Effects: &spec.PolicyEffects{Permit: []string{"production.read"}},
			},
		},
	}
	g.Workflows["deploy-production"].Spec.Policy = "staging-only"
	g.Agents["deploy-agent"].Spec.Policy = "staging-only"
	err := Check(g)
	if err == nil {
		t.Fatal("expected unpermitted effect")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Error: effect not permitted by policy") {
		t.Fatalf("header: %s", msg)
	}
	if !strings.Contains(msg, "Workflow/deploy-production may perform effect `production.write`") {
		t.Fatalf("workflow line: %s", msg)
	}
	if !strings.Contains(msg, "AUTONOMOUS") {
		t.Fatalf("AUTONOMOUS missing: %s", msg)
	}
	if !strings.Contains(msg, "Agent/deploy-agent") {
		t.Fatalf("agent hop: %s", msg)
	}
	if !strings.Contains(msg, "tool.kubernetes.restart") {
		t.Fatalf("uses: %s", msg)
	}
	if !strings.Contains(msg, "Policy/staging-only permits: production.read") {
		t.Fatalf("policy named: %s", msg)
	}
}

func TestCheck_failClosedNamesPolicy(t *testing.T) {
	t.Parallel()
	g := graph(
		toolsGithub(),
		nil,
		workflow("review", stepUses("fetch", "tool.github.read_pr")),
	)
	g.Policies = map[string]*spec.PolicyResource{
		"guarded-writes": {Metadata: spec.Metadata{Name: "guarded-writes"}},
	}
	g.Workflows["review"].Spec.Policy = "guarded-writes"
	err := Check(g)
	if err == nil {
		t.Fatal("expected fail-closed")
	}
	if !strings.Contains(err.Error(), "Policy/guarded-writes") {
		t.Fatalf("must name the policy: %v", err)
	}
	if !strings.Contains(err.Error(), "permits: (none)") {
		t.Fatalf("empty permit: %v", err)
	}
}

func TestCheck_prefixPermitCovers(t *testing.T) {
	t.Parallel()
	g := graph(
		toolsGithub(),
		nil,
		workflow("review", stepUses("fetch", "tool.github.read_pr")),
	)
	g.Policies = map[string]*spec.PolicyResource{
		"guarded": {
			Metadata: spec.Metadata{Name: "guarded"},
			Spec: spec.PolicySpec{
				Effects: &spec.PolicyEffects{Permit: []string{"github"}},
			},
		},
	}
	g.Workflows["review"].Spec.Policy = "guarded"
	if err := Check(g); err != nil {
		t.Fatal(err)
	}
}

func TestCheck_undeclaredUnknown(t *testing.T) {
	t.Parallel()
	g := graph(
		map[string]*spec.ToolResource{
			"helper": {Metadata: spec.Metadata{Name: "helper"}, Spec: spec.ToolSpec{Type: "native"}},
			"github": toolsGithub()["github"],
		},
		nil,
		workflow("job", stepUses("ping", "tool.helper.echo")),
	)
	g.Policies = map[string]*spec.PolicyResource{
		"p": {
			Metadata: spec.Metadata{Name: "p"},
			Spec:     spec.PolicySpec{Effects: &spec.PolicyEffects{Permit: []string{"github.read"}}},
		},
	}
	g.Workflows["job"].Spec.Policy = "p"
	err := Check(g)
	if err == nil {
		t.Fatal("unknown reachable op must fail closed")
	}
	if !strings.Contains(err.Error(), "unknown effect") {
		t.Fatalf("unknown: %v", err)
	}
	if !strings.Contains(err.Error(), "Tool/helper") {
		t.Fatalf("must name the tool: %v", err)
	}
}

func TestCheck_stricterRequiresApprovalWins(t *testing.T) {
	t.Parallel()
	g := graph(
		map[string]*spec.ToolResource{
			"github": {
				Metadata: spec.Metadata{Name: "github"},
				Spec: spec.ToolSpec{
					Type: "native",
					Safety: &spec.ToolSafety{
						RequiresApproval: spec.BoolPtr(true),
					},
					Operations: map[string]spec.ToolOperation{
						"merge_pr": {Effects: []string{"destructive"}},
					},
				},
			},
		},
		nil,
		workflow("ship", stepUses("merge", "tool.github.merge_pr")),
	)
	g.Policies = map[string]*spec.PolicyResource{
		"guarded": {
			Metadata: spec.Metadata{Name: "guarded"},
			Spec: spec.PolicySpec{
				Effects: &spec.PolicyEffects{Permit: []string{"destructive"}},
			},
		},
	}
	g.Workflows["ship"].Spec.Policy = "guarded"
	err := Check(g)
	if err == nil {
		t.Fatal("permit (unattended) must lose to requiresApproval")
	}
	if !strings.Contains(err.Error(), "stricter rule applied (requiresApproval)") {
		t.Fatalf("stricter: %v", err)
	}

	g.Policies["guarded"].Spec.Effects.Permit = nil
	g.Policies["guarded"].Spec.Effects.PermitWithApproval = []string{"destructive"}
	if err := Check(g); err != nil {
		t.Fatalf("permitWithApproval should agree with requiresApproval: %v", err)
	}
}

func TestCheck_dualListPermitWithApprovalWins(t *testing.T) {
	t.Parallel()
	g := graph(
		map[string]*spec.ToolResource{
			"github": {
				Metadata: spec.Metadata{Name: "github"},
				Spec: spec.ToolSpec{
					Type: "native",
					Safety: &spec.ToolSafety{
						RequiresApproval: spec.BoolPtr(true),
					},
					Operations: map[string]spec.ToolOperation{
						"merge_pr": {Effects: []string{"github.write"}},
					},
				},
			},
		},
		nil,
		workflow("ship", stepUses("merge", "tool.github.merge_pr")),
	)
	g.Policies = map[string]*spec.PolicyResource{
		"guarded": {
			Metadata: spec.Metadata{Name: "guarded"},
			Spec: spec.PolicySpec{
				Effects: &spec.PolicyEffects{
					Permit:             []string{"github.write"},
					PermitWithApproval: []string{"github.write"},
				},
			},
		},
	}
	g.Workflows["ship"].Spec.Policy = "guarded"
	if err := Check(g); err != nil {
		t.Fatalf("same ident in both lists is approval-gated; unattended permit must not win: %v", err)
	}

	g.Policies["guarded"].Spec.Effects.PermitWithApproval = nil
	err := Check(g)
	if err == nil {
		t.Fatal("permit-only (unattended) must lose to requiresApproval")
	}
	if !strings.Contains(err.Error(), "stricter rule applied (requiresApproval)") {
		t.Fatalf("stricter: %v", err)
	}
}

func TestCheck_anyReachableOpRequiresApprovalWins(t *testing.T) {
	t.Parallel()
	g := graph(
		map[string]*spec.ToolResource{
			"github": {
				Metadata: spec.Metadata{Name: "github"},
				Spec: spec.ToolSpec{
					Type: "native",
					Safety: &spec.ToolSafety{
						Trusted:     spec.BoolPtr(true),
						SideEffects: spec.BoolPtr(false),
					},
					Operations: map[string]spec.ToolOperation{
						"post_comment": {Effects: []string{"github.write"}},
						"merge_pr":     {Effects: []string{"github.write"}},
					},
				},
			},
		},
		nil,
		workflow("ship",
			stepUses("comment", "tool.github.post_comment"),
			stepUses("merge", "tool.github.merge_pr"),
		),
	)
	g.Policies = map[string]*spec.PolicyResource{
		"guarded": {
			Metadata: spec.Metadata{Name: "guarded"},
			Spec: spec.PolicySpec{
				Approvals: &spec.PolicyApprovals{RequiredFor: []string{"tool.github.merge_pr"}},
				Effects:   &spec.PolicyEffects{Permit: []string{"github.write"}},
			},
		},
	}
	g.Workflows["ship"].Spec.Policy = "guarded"
	err := Check(g)
	if err == nil {
		t.Fatal("first-witness post_comment must not hide merge_pr requiredFor")
	}
	if !strings.Contains(err.Error(), "stricter rule applied (requiresApproval)") {
		t.Fatalf("stricter: %v", err)
	}
	if !strings.Contains(err.Error(), "tool.github.merge_pr") {
		t.Fatalf("must name the approval-gated uses: %v", err)
	}

	g.Policies["guarded"].Spec.Effects.Permit = nil
	g.Policies["guarded"].Spec.Effects.PermitWithApproval = []string{"github.write"}
	if err := Check(g); err != nil {
		t.Fatalf("permitWithApproval covers any-op approval: %v", err)
	}
}

func TestCheck_staticUsesPermitted(t *testing.T) {
	t.Parallel()
	g := graph(
		toolsGithub(),
		nil,
		workflow("review", stepUses("fetch", "tool.github.read_pr")),
	)
	g.Policies = map[string]*spec.PolicyResource{
		"guarded": {
			Metadata: spec.Metadata{Name: "guarded"},
			Spec: spec.PolicySpec{
				Effects: &spec.PolicyEffects{Permit: []string{"github.read"}},
			},
		},
	}
	g.Workflows["review"].Spec.Policy = "guarded"
	if err := Check(g); err != nil {
		t.Fatal(err)
	}
}

func TestCheck_errorsJoin(t *testing.T) {
	t.Parallel()
	err := Check(graph(toolsGithub(), nil, workflow("review", stepUses("fetch", "tool.github.read_pr"))))
	if err == nil {
		t.Fatal("no policy + declared effects")
	}
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) && !strings.Contains(err.Error(), "Policy/(none)") {
		// single or joined; must name missing policy
		if !strings.Contains(err.Error(), "(none)") {
			t.Fatalf("%v", err)
		}
	}
}
