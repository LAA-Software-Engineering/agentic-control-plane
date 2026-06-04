package plan

import (
	"context"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestRiskSummary_policyLint_sensitiveTool(t *testing.T) {
	g := minimalGraph()
	g.Tools["delete_records"] = &spec.ToolResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindTool,
		Metadata:   spec.Metadata{Name: "delete_records"},
		Spec:       spec.ToolSpec{Type: "native"},
	}
	g.Policies["default"] = &spec.PolicyResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindPolicy,
		Metadata:   spec.Metadata{Name: "default"},
		Spec:       spec.PolicySpec{},
	}
	spec.NormalizeProjectGraph(g)

	p := NewPlanner(&fakeDeploy{list: nil})
	pl, err := p.ComputePlan(context.Background(), "dev", g)
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.Risk.Lint) == 0 {
		t.Fatal("expected structured lint findings")
	}
	var found bool
	for _, f := range pl.Risk.Lint {
		if f.Rule == policy.LintRuleUngatedSensitiveTool {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("lint = %#v", pl.Risk.Lint)
	}
	joined := strings.Join(pl.Risk.Messages, " ")
	if !strings.Contains(joined, "ungated") && !strings.Contains(joined, "explicit approval rule") {
		t.Fatalf("messages = %#v", pl.Risk.Messages)
	}
}
