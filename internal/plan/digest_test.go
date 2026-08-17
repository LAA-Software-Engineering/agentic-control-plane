package plan

import (
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestResolvedGraphDigest_stable(t *testing.T) {
	g := &spec.ProjectGraph{
		Meta: spec.Metadata{Name: "demo"},
		Spec: spec.ProjectSpec{
			State: &spec.ProjectStateConfig{Backend: "sqlite", DSN: ".agentic/state.db"},
		},
		Agents: map[string]*spec.AgentResource{
			"a": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindAgent,
				Metadata:   spec.Metadata{Name: "a"},
				Spec:       spec.AgentSpec{Model: "openai/gpt-4"},
			},
		},
	}
	d1, err := ResolvedGraphDigest(g)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := ResolvedGraphDigest(g)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 || d1 == "" {
		t.Fatalf("digest = %q, want stable non-empty", d1)
	}
}

func TestResolvedGraphDigest_changesWithGraph(t *testing.T) {
	g := &spec.ProjectGraph{
		Meta: spec.Metadata{Name: "demo"},
		Spec: spec.ProjectSpec{},
	}
	d1, err := ResolvedGraphDigest(g)
	if err != nil {
		t.Fatal(err)
	}
	g.Agents = map[string]*spec.AgentResource{
		"x": {
			APIVersion: spec.APIVersionV0,
			Kind:       spec.KindAgent,
			Metadata:   spec.Metadata{Name: "x"},
			Spec:       spec.AgentSpec{},
		},
	}
	d2, err := ResolvedGraphDigest(g)
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d2 {
		t.Fatal("digest should change when graph changes")
	}
}

func TestResolvedGraphDigest_changesWithLimits(t *testing.T) {
	g := &spec.ProjectGraph{
		Meta: spec.Metadata{Name: "demo"},
		Spec: spec.ProjectSpec{},
	}
	d1, err := ResolvedGraphDigest(g)
	if err != nil {
		t.Fatal(err)
	}
	g.Spec.Limits = &spec.ExecutionLimits{MaxToolOutputBytes: 1024}
	d2, err := ResolvedGraphDigest(g)
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d2 {
		t.Fatal("digest should change when spec.limits is added")
	}
}

func TestResolvedGraphDigest_ignoresPos(t *testing.T) {
	wf := &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindWorkflow,
		Metadata:   spec.Metadata{Name: "demo"},
		Spec: spec.WorkflowSpec{
			Steps: []spec.WorkflowStep{{ID: "a", Uses: "tool.x.y"}},
		},
	}
	agent := &spec.AgentResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindAgent,
		Metadata:   spec.Metadata{Name: "bot"},
		Spec:       spec.AgentSpec{Model: "mock/gpt-4", Tools: []string{"x"}},
	}
	g := &spec.ProjectGraph{
		Meta: spec.Metadata{Name: "demo"},
		Spec: spec.ProjectSpec{},
		Agents: map[string]*spec.AgentResource{
			"bot": agent,
		},
		Workflows: map[string]*spec.WorkflowResource{
			"demo": wf,
		},
		Policies: map[string]*spec.PolicyResource{
			"default": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindPolicy,
				Metadata:   spec.Metadata{Name: "default"},
				Spec: spec.PolicySpec{
					Approvals: &spec.PolicyApprovals{
						RequiredFor: []string{"tool.x.y"},
					},
					Hitl: &spec.HitlPolicy{
						InterruptOn: map[string]spec.HitlInterruptValue{
							"x": {Enabled: true},
						},
					},
					Effects: &spec.PolicyEffects{
						Permit: []string{"github.read"},
					},
				},
			},
		},
		Tools: map[string]*spec.ToolResource{
			"x": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindTool,
				Metadata:   spec.Metadata{Name: "x"},
				Spec: spec.ToolSpec{
					Type: "native",
					Operations: map[string]spec.ToolOperation{
						"y": {Effects: []string{"github.read"}},
					},
				},
			},
		},
	}

	d1, err := ResolvedGraphDigest(g)
	if err != nil {
		t.Fatal(err)
	}
	j1, err := canonicalResourceJSON(wf)
	if err != nil {
		t.Fatal(err)
	}
	h1, err := WorkflowSpecHash(wf)
	if err != nil {
		t.Fatal(err)
	}

	agent.Pos = spec.Pos{File: "agent.yaml", Line: 1, Column: 1}
	agent.Spec.ToolsPos = []spec.Pos{{File: "agent.yaml", Line: 12, Column: 5}}
	wf.Pos = spec.Pos{File: "workflow.yaml", Line: 1, Column: 1}
	wf.Spec.Steps[0].Pos = spec.Pos{File: "workflow.yaml", Line: 8, Column: 5}
	wf.Spec.Steps[0].UsesPos = spec.Pos{File: "workflow.yaml", Line: 9, Column: 13}
	g.Pos = spec.Pos{File: "project.yaml", Line: 1, Column: 1}
	pol := g.Policies["default"]
	pol.Pos = spec.Pos{File: "policy.yaml", Line: 1, Column: 1}
	pol.Spec.Approvals.RequiredForPos = []spec.Pos{{File: "policy.yaml", Line: 8, Column: 7}}
	pol.Spec.Hitl.InterruptOnPos = map[string]spec.Pos{
		"x": {File: "policy.yaml", Line: 11, Column: 5},
	}
	pol.Spec.Effects.PermitPos = []spec.Pos{{File: "policy.yaml", Line: 20, Column: 7}}
	tool := g.Tools["x"]
	op := tool.Spec.Operations["y"]
	op.Pos = spec.Pos{File: "tool.yaml", Line: 10, Column: 3}
	op.EffectsPos = []spec.Pos{{File: "tool.yaml", Line: 11, Column: 16}}
	tool.Spec.Operations["y"] = op
	tool.Pos = spec.Pos{File: "tool.yaml", Line: 1, Column: 1}

	d2, err := ResolvedGraphDigest(g)
	if err != nil {
		t.Fatal(err)
	}
	j2, err := canonicalResourceJSON(wf)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := WorkflowSpecHash(wf)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("ResolvedGraphDigest changed after Pos mutation: %s vs %s", d1, d2)
	}
	if string(j1) != string(j2) {
		t.Fatalf("canonicalResourceJSON changed after Pos mutation:\n%s\n%s", j1, j2)
	}
	if h1 != h2 {
		t.Fatalf("WorkflowSpecHash changed after Pos mutation: %s vs %s", h1, h2)
	}

	wf.Spec.Steps[0].Pos.Line = 99
	wf.Spec.Steps[0].UsesPos.Column = 42
	d3, err := ResolvedGraphDigest(g)
	if err != nil {
		t.Fatal(err)
	}
	if d3 != d1 {
		t.Fatal("mutating Line/Column must not dirty ResolvedGraphDigest")
	}
}
