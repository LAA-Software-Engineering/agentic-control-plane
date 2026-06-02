package spec

import (
	"strings"
	"testing"
)

func TestValidateProjectGraph_unknownPresetOnPolicy(t *testing.T) {
	g := &ProjectGraph{
		Policies: map[string]*PolicyResource{
			"custom": {
				Metadata: Metadata{Name: "custom"},
				Spec: PolicySpec{
					Preset: "bogus",
				},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), `unknown preset "bogus"`) {
		t.Fatalf("got %v", err)
	}
}

func TestValidateProjectGraph_defaultsPolicyBuiltinPreset(t *testing.T) {
	g := &ProjectGraph{
		Spec: ProjectSpec{
			Defaults: &ProjectDefaults{Policy: PresetShellSafe},
		},
	}
	NormalizeProjectGraph(g)
	if err := ValidateProjectGraph(g, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if pr, ok := g.Policies[PresetShellSafe]; !ok || pr.Spec.ResolvedPreset != PresetShellSafe {
		t.Fatalf("expected expanded shell_safe policy, got %+v", g.Policies[PresetShellSafe])
	}
}

func TestNormalizeProjectGraph_expandsPolicyPreset(t *testing.T) {
	g := &ProjectGraph{
		Policies: map[string]*PolicyResource{
			"team": {
				Metadata: Metadata{Name: "team"},
				Spec: PolicySpec{
					Preset: PresetStrict,
					Execution: &PolicyExecution{
						MaxWallClockSeconds: 120,
					},
				},
			},
		},
	}
	NormalizeProjectGraph(g)
	sp := g.Policies["team"].Spec
	if sp.ResolvedPreset != PresetStrict {
		t.Fatalf("ResolvedPreset = %q", sp.ResolvedPreset)
	}
	if sp.Approvals == nil || !sp.Approvals.RequireAllTools {
		t.Fatalf("strict expansion: %+v", sp.Approvals)
	}
	if sp.Execution == nil || sp.Execution.MaxWallClockSeconds != 120 {
		t.Fatalf("local execution override lost: %+v", sp.Execution)
	}
}

func TestResolveReferences_builtinPresetPolicyOK(t *testing.T) {
	g := &ProjectGraph{
		Spec: ProjectSpec{
			Defaults: &ProjectDefaults{Policy: PresetPermissive},
		},
		Workflows: map[string]*WorkflowResource{
			"wf": {
				Metadata: Metadata{Name: "wf"},
				Spec:     WorkflowSpec{},
			},
		},
	}
	NormalizeProjectGraph(g)
	if err := ResolveReferences(g); err != nil {
		t.Fatal(err)
	}
}
