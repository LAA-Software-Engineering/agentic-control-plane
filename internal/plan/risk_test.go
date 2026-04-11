package plan

import (
	"context"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestActionSuggestsWriteSideEffects(t *testing.T) {
	tests := []struct {
		action string
		want   bool
	}{
		{"issues.write", true},
		{"pull_requests.read", false},
		{"tool.github.pull_request.merge", true},
		{"tool.slack.message.send", true},
		{"contents.read", false},
	}
	for _, tt := range tests {
		if got := ActionSuggestsWriteSideEffects(tt.action); got != tt.want {
			t.Errorf("%q: got %v want %v", tt.action, got, tt.want)
		}
	}
}

func graphWithPolicy(cost float64) *spec.ProjectGraph {
	g := minimalGraph()
	g.Policies["default"] = &spec.PolicyResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindPolicy,
		Metadata:   spec.Metadata{Name: "default"},
		Spec: spec.PolicySpec{
			Execution: &spec.PolicyExecution{MaxTotalCostUsd: cost},
		},
	}
	return g
}

func graphWithTool(allow []string) *spec.ProjectGraph {
	g := minimalGraph()
	g.Tools["github"] = &spec.ToolResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindTool,
		Metadata:   spec.Metadata{Name: "github"},
		Spec: spec.ToolSpec{
			Type: "mcp",
			Permissions: &spec.ToolPermissions{
				Allow: allow,
			},
		},
	}
	return g
}

func TestRiskSummary_costCeilingIncreased(t *testing.T) {
	oldG := graphWithPolicy(3.0)
	applied := appliedFromDesired(t, "dev", oldG)
	newG := graphWithPolicy(10.0)

	p := NewPlanner(&fakeDeploy{list: applied})
	pl, err := p.ComputePlan(context.Background(), "dev", newG)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(pl.Risk.Messages, " ")
	if !strings.Contains(strings.ToLower(joined), "cost ceiling increased") {
		t.Fatalf("expected cost ceiling risk, got %#v", pl.Risk.Messages)
	}
}

func TestRiskSummary_newToolCreate_flagsWriteLikeWhenNoPriorState(t *testing.T) {
	g := graphWithTool([]string{"issues.write"})
	p := NewPlanner(&fakeDeploy{list: nil})
	pl, err := p.ComputePlan(context.Background(), "dev", g)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range pl.Risk.Messages {
		if strings.Contains(strings.ToLower(m), "write") && strings.Contains(strings.ToLower(m), "permission") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected baseline tool permission risk, got %#v", pl.Risk.Messages)
	}
}

func TestRiskSummary_newWriteLikeToolPermissions(t *testing.T) {
	oldG := graphWithTool([]string{"contents.read"})
	applied := appliedFromDesired(t, "dev", oldG)
	newG := graphWithTool([]string{"contents.read", "issues.write"})

	p := NewPlanner(&fakeDeploy{list: applied})
	pl, err := p.ComputePlan(context.Background(), "dev", newG)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range pl.Risk.Messages {
		if strings.Contains(strings.ToLower(m), "write") && strings.Contains(strings.ToLower(m), "permission") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected write-permission risk, got %#v", pl.Risk.Messages)
	}
}
