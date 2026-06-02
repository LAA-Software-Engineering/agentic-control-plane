package spec_test

import (
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"gopkg.in/yaml.v3"
)

func TestHitlInterruptValue_unmarshalTrue(t *testing.T) {
	t.Helper()
	var m map[string]spec.HitlInterruptValue
	if err := yaml.Unmarshal([]byte("helper: true"), &m); err != nil {
		t.Fatal(err)
	}
	v, ok := m["helper"]
	if !ok || !v.Enabled || v.Config != nil {
		t.Fatalf("got %+v", v)
	}
}

func TestHitlInterruptValue_unmarshalConfig(t *testing.T) {
	t.Helper()
	raw := `
helper:
  allowedDecisions: [approve, reject, edit]
  deniedEditArgs: [secret]
`
	var m map[string]spec.HitlInterruptValue
	if err := yaml.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	v := m["helper"]
	if v.Config == nil {
		t.Fatal("expected config")
	}
	if len(v.Config.AllowedDecisions) != 3 {
		t.Fatalf("decisions: %v", v.Config.AllowedDecisions)
	}
}

func TestHitlInterruptValue_unmarshalFalseRejected(t *testing.T) {
	t.Helper()
	var m map[string]spec.HitlInterruptValue
	err := yaml.Unmarshal([]byte("helper: false"), &m)
	if err == nil || !strings.Contains(err.Error(), "false") {
		t.Fatalf("expected false rejection, got %v", err)
	}
}

func TestParseHitlDecisionKind(t *testing.T) {
	t.Helper()
	for _, tc := range []struct {
		in   string
		want spec.HitlDecisionKind
		ok   bool
	}{
		{"approve", spec.HitlDecisionApprove, true},
		{"REJECT", spec.HitlDecisionReject, true},
		{"edit", spec.HitlDecisionEdit, true},
		{"switch", spec.HitlDecisionSwitch, true},
		{"nope", "", false},
	} {
		got, err := spec.ParseHitlDecisionKind(tc.in)
		if tc.ok && err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("%q: expected error", tc.in)
		}
		if tc.ok && got != tc.want {
			t.Fatalf("%q: got %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidatePolicySpecs_hitlOverlap(t *testing.T) {
	t.Helper()
	g := &spec.ProjectGraph{
		Policies: map[string]*spec.PolicyResource{
			"bad": {
				Spec: spec.PolicySpec{
					Hitl: &spec.HitlPolicy{
						InterruptOn: map[string]spec.HitlInterruptValue{
							"helper": {
								Enabled: true,
								Config: &spec.HitlInterruptConfig{
									AllowedEditArgs: []string{"x", "y"},
									DeniedEditArgs:  []string{"y"},
								},
							},
						},
					},
				},
			},
		},
	}
	err := spec.ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("expected overlap error, got %v", err)
	}
}

func TestValidatePolicySpecs_hitlInvalidDecision(t *testing.T) {
	t.Helper()
	g := &spec.ProjectGraph{
		Policies: map[string]*spec.PolicyResource{
			"bad": {
				Spec: spec.PolicySpec{
					Hitl: &spec.HitlPolicy{
						InterruptOn: map[string]spec.HitlInterruptValue{
							"helper": {
								Enabled: true,
								Config: &spec.HitlInterruptConfig{
									AllowedDecisions: []spec.HitlDecisionKind{"maybe"},
								},
							},
						},
					},
				},
			},
		},
	}
	err := spec.ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unknown decision") {
		t.Fatalf("expected unknown decision error, got %v", err)
	}
}
