package policy

import (
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestLint_ungatedSensitiveTool_noExplicitRule(t *testing.T) {
	t.Helper()
	g := testGraphWithTools("delete_records")
	g.Policies = map[string]*spec.PolicyResource{
		"default": {
			Metadata: spec.Metadata{Name: "default"},
			Spec:     spec.PolicySpec{},
		},
	}
	spec.NormalizeProjectGraph(g)

	findings := Lint(g)
	if !containsLintRule(findings, LintRuleUngatedSensitiveTool) {
		t.Fatalf("expected ungated_sensitive_tool, got %#v", findings)
	}
	if !HasHighSeverityLint(findings) {
		t.Fatal("expected high severity")
	}
}

func TestLint_ungatedSensitiveTool_permissive(t *testing.T) {
	t.Helper()
	g := testGraphWithTools("delete_records")
	permissive := true
	g.Policies = map[string]*spec.PolicyResource{
		"open": {
			Metadata: spec.Metadata{Name: "open"},
			Spec: spec.PolicySpec{
				Approvals: &spec.PolicyApprovals{Permissive: &permissive},
			},
		},
	}
	spec.NormalizeProjectGraph(g)

	findings := Lint(g)
	var msg string
	for _, f := range findings {
		if f.Rule == LintRuleUngatedSensitiveTool && strings.Contains(f.Message, "permissive") {
			msg = f.Message
			break
		}
	}
	if msg == "" {
		t.Fatalf("expected permissive finding, got %#v", findings)
	}
}

func TestLint_ungatedSensitiveTool_explicitRuleOK(t *testing.T) {
	t.Helper()
	g := testGraphWithTools("delete_records")
	g.Policies = map[string]*spec.PolicyResource{
		"default": {
			Metadata: spec.Metadata{Name: "default"},
			Spec: spec.PolicySpec{
				Approvals: &spec.PolicyApprovals{
					RequiredFor: []string{"tool.delete_records.purge"},
				},
			},
		},
	}
	spec.NormalizeProjectGraph(g)

	findings := Lint(g)
	for _, f := range findings {
		if f.Rule == LintRuleUngatedSensitiveTool && f.Tool == "delete_records" {
			t.Fatalf("unexpected finding: %#v", f)
		}
	}
}

func TestLint_unknownRequiredForRef(t *testing.T) {
	t.Helper()
	g := testGraphWithTools("helper")
	g.Policies = map[string]*spec.PolicyResource{
		"default": {
			Metadata: spec.Metadata{Name: "default"},
			Spec: spec.PolicySpec{
				Approvals: &spec.PolicyApprovals{
					RequiredFor: []string{"tool.missing.op"},
				},
			},
		},
	}
	findings := Lint(g)
	if !containsLintRule(findings, LintRuleUnknownRequiredForRef) {
		t.Fatalf("got %#v", findings)
	}
}

func TestLint_invalidSwitchTarget(t *testing.T) {
	t.Helper()
	g := testGraphWithTools("deploy")
	g.Tools["deploy"].Spec.Type = "native"
	g.Policies = map[string]*spec.PolicyResource{
		"default": {
			Metadata: spec.Metadata{Name: "default"},
			Spec: spec.PolicySpec{
				Hitl: &spec.HitlPolicy{
					InterruptOn: map[string]spec.HitlInterruptValue{
						"deploy": {Enabled: true},
					},
					ToolSwitchMap: map[string][]string{
						"deploy_to_production": {"nonexistent_operation"},
					},
				},
			},
		},
	}
	findings := Lint(g)
	if !containsLintRule(findings, LintRuleInvalidSwitchTarget) {
		t.Fatalf("got %#v", findings)
	}
}

func TestLint_unknownEditArg(t *testing.T) {
	t.Helper()
	g := testGraphWithTools("helper")
	g.Tools["helper"].Spec.Type = "native"
	g.Tools["helper"].Spec.Safety = &spec.ToolSafety{SideEffects: spec.BoolPtr(false)}
	g.Workflows = map[string]*spec.WorkflowResource{
		"demo": {
			Metadata: spec.Metadata{Name: "demo"},
			Spec: spec.WorkflowSpec{
				Steps: []spec.WorkflowStep{{
					ID:   "s1",
					Uses: "tool.helper.identity",
				}},
			},
		},
	}
	g.Policies = map[string]*spec.PolicyResource{
		"default": {
			Metadata: spec.Metadata{Name: "default"},
			Spec: spec.PolicySpec{
				Hitl: &spec.HitlPolicy{
					InterruptOn: map[string]spec.HitlInterruptValue{
						"helper": {
							Enabled: true,
							Config: &spec.HitlInterruptConfig{
								DeniedEditArgs: []string{"secret"},
							},
						},
					},
				},
			},
		},
	}
	findings := Lint(g)
	if !containsLintRule(findings, LintRuleUnknownEditArg) {
		t.Fatalf("got %#v", findings)
	}
}

func TestLint_presetWeakened_strictToPermissive(t *testing.T) {
	t.Helper()
	permissive := true
	g := &spec.ProjectGraph{
		Tools: testGraphWithTools("helper").Tools,
		Policies: map[string]*spec.PolicyResource{
			"relaxed": {
				Metadata: spec.Metadata{Name: "relaxed"},
				Spec: spec.PolicySpec{
					Preset: spec.PresetStrict,
					Approvals: &spec.PolicyApprovals{
						Permissive: &permissive,
					},
				},
			},
		},
	}
	spec.NormalizeProjectGraph(g)
	findings := Lint(g)
	if !containsLintRule(findings, LintRulePresetWeakened) {
		t.Fatalf("got %#v", findings)
	}
}

func TestLint_unreachableRequiredFor(t *testing.T) {
	t.Helper()
	g := testGraphWithTools("helper")
	requireAll := true
	g.Policies = map[string]*spec.PolicyResource{
		"default": {
			Metadata: spec.Metadata{Name: "default"},
			Spec: spec.PolicySpec{
				Approvals: &spec.PolicyApprovals{
					RequireAllTools: &requireAll,
					RequiredFor:     []string{"tool.helper.echo"},
				},
			},
		},
	}
	findings := Lint(g)
	if !containsLintRule(findings, LintRuleUnreachableRequiredFor) {
		t.Fatalf("got %#v", findings)
	}
}

func TestLint_safeToolNoFindings(t *testing.T) {
	t.Helper()
	g := testGraphWithTools("helper")
	g.Tools["helper"].Spec.Safety = &spec.ToolSafety{
		SideEffects: spec.BoolPtr(false),
	}
	g.Policies = map[string]*spec.PolicyResource{
		"default": {
			Metadata: spec.Metadata{Name: "default"},
			Spec:     spec.PolicySpec{},
		},
	}
	spec.NormalizeProjectGraph(g)
	findings := Lint(g)
	for _, f := range findings {
		if f.Rule == LintRuleUngatedSensitiveTool {
			t.Fatalf("unexpected %#v", f)
		}
	}
}

func TestHasHighSeverityLint(t *testing.T) {
	if HasHighSeverityLint(nil) {
		t.Fatal("nil should be false")
	}
	if !HasHighSeverityLint([]LintFinding{{Severity: LintSeverityHigh}}) {
		t.Fatal("expected true")
	}
	if HasHighSeverityLint([]LintFinding{{Severity: LintSeverityLow}}) {
		t.Fatal("expected false")
	}
}

func containsLintRule(findings []LintFinding, rule LintRule) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}
