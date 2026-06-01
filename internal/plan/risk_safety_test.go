package plan

import (
	"context"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestRiskSummary_newTool_flagsSafetyApproval(t *testing.T) {
	g := minimalGraph()
	g.Tools["risky"] = &spec.ToolResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindTool,
		Metadata:   spec.Metadata{Name: "risky"},
		Spec:       spec.ToolSpec{Type: "native"},
	}
	spec.NormalizeProjectGraph(g)

	p := NewPlanner(&fakeDeploy{list: nil})
	pl, err := p.ComputePlan(context.Background(), "dev", g)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range pl.Risk.Messages {
		if strings.Contains(m, "Tool/risky") && strings.Contains(m, "require approval") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected safety approval risk, got %#v", pl.Risk.Messages)
	}
}

func TestRiskSummary_trustedTool_noSafetyApprovalRisk(t *testing.T) {
	g := minimalGraph()
	trusted := true
	g.Tools["safe"] = &spec.ToolResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindTool,
		Metadata:   spec.Metadata{Name: "safe"},
		Spec: spec.ToolSpec{
			Type:   "native",
			Safety: &spec.ToolSafety{Trusted: &trusted},
		},
	}
	spec.NormalizeProjectGraph(g)

	p := NewPlanner(&fakeDeploy{list: nil})
	pl, err := p.ComputePlan(context.Background(), "dev", g)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range pl.Risk.Messages {
		if strings.Contains(m, "Tool/safe") && strings.Contains(m, "require approval") {
			t.Fatalf("unexpected approval risk: %s", m)
		}
	}
}
