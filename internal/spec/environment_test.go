package spec

import (
	"testing"
)

func testEnvGraph(t *testing.T) *ProjectGraph {
	t.Helper()
	g := &ProjectGraph{
		Meta: Metadata{Name: "env-test"},
		Agents: map[string]*AgentResource{
			"reviewer": {
				Metadata: Metadata{Name: "reviewer"},
				Spec: AgentSpec{
					Model:  "mock/gpt-4",
					Policy: "default",
				},
			},
		},
		Policies: map[string]*PolicyResource{
			"default": {
				Metadata: Metadata{Name: "default"},
				Spec: PolicySpec{
					Execution: &PolicyExecution{
						MaxWallClockSeconds: 300,
						MaxTotalCostUsd:     5,
					},
					Approvals: &PolicyApprovals{
						RequiredFor: []string{"tool.github.pull_request.post_comment"},
					},
				},
			},
		},
		Environments: map[string]*EnvironmentResource{
			"prod": {
				Metadata: Metadata{Name: "prod"},
				Spec: EnvironmentSpec{
					Overrides: &EnvironmentOverrides{
						Agents: map[string]AgentOverride{
							"reviewer": {Model: "openai/gpt-4o"},
						},
						Policies: map[string]PolicyOverride{
							"default": {
								Execution: &PolicyExecution{
									MaxTotalCostUsd:         1,
									MaxWallClockSeconds:     60,
									RequireStructuredOutput: true,
								},
								Approvals: &PolicyApprovals{
									RequiredFor: []string{"tool.notify.default"},
								},
							},
						},
					},
				},
			},
		},
	}
	return g
}

func TestApplyEnvironment_policyApprovalsUnion(t *testing.T) {
	g := testEnvGraph(t)
	baseReq := append([]string(nil), g.Policies["default"].Spec.Approvals.RequiredFor...)

	out, err := ApplyEnvironment(g, "prod")
	if err != nil {
		t.Fatal(err)
	}
	pol := out.Policies["default"]
	if pol == nil || pol.Spec.Approvals == nil {
		t.Fatal("missing merged approvals")
	}
	got := pol.Spec.Approvals.RequiredFor
	if len(got) != 2 || got[0] != "tool.github.pull_request.post_comment" || got[1] != "tool.notify.default" {
		t.Fatalf("requiredFor = %#v", got)
	}
	if pol.Spec.Execution == nil || pol.Spec.Execution.MaxTotalCostUsd != 1 || !pol.Spec.Execution.RequireStructuredOutput {
		t.Fatalf("execution %+v", pol.Spec.Execution)
	}
	if out.Agents["reviewer"].Spec.Model != "openai/gpt-4o" {
		t.Fatalf("model = %q", out.Agents["reviewer"].Spec.Model)
	}

	if orig := g.Policies["default"].Spec.Approvals.RequiredFor; len(orig) != len(baseReq) || orig[0] != baseReq[0] {
		t.Fatalf("base graph mutated: %#v", orig)
	}
}

func TestApplyEnvironment_emptyApprovalsOverlayIsNoop(t *testing.T) {
	g := testEnvGraph(t)
	g.Environments["prod"].Spec.Overrides.Policies["default"] = PolicyOverride{
		Approvals: &PolicyApprovals{RequiredFor: nil},
	}
	out, err := ApplyEnvironment(g, "prod")
	if err != nil {
		t.Fatal(err)
	}
	got := out.Policies["default"].Spec.Approvals.RequiredFor
	if len(got) != 1 || got[0] != "tool.github.pull_request.post_comment" {
		t.Fatalf("requiredFor = %#v", got)
	}
}

func TestApplyEnvironment_unknownEnv(t *testing.T) {
	_, err := ApplyEnvironment(testEnvGraph(t), "missing")
	if err == nil {
		t.Fatal("expected error")
	}
}
