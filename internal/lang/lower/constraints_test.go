package lower_test

import (
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/lang"
	"github.com/LAA-Software-Engineering/terfyn/internal/lang/lower"
)

// TestLower_DescriptionAndConstraints asserts the .agent description/constraints
// fields lower into the existing AgentSpec/WorkflowSpec targets (#310).
func TestLower_DescriptionAndConstraints(t *testing.T) {
	src := "agent Responder {\n" +
		"    model mock/gpt-4\n" +
		"    description \"handles incidents\"\n" +
		"    constraints {\n        maxIterations 8\n        timeoutSeconds 120\n    }\n" +
		"}\n\n" +
		"workflow triage(input: Alert) -> Status\n    description \"triage an alert\"\n{\n    r = Responder(alert: input.alert)\n    return r\n}\n"
	f, diags := lang.Parse("t.agent", src)
	if len(diags) > 0 {
		t.Fatalf("parse diags: %s", diags.Error())
	}
	res, ld := lower.LowerFile(f, lower.Options{})
	if len(ld) > 0 {
		t.Fatalf("lower diags: %s", ld.Error())
	}
	ar := res.Agents[0]
	if ar.Spec.Description != "handles incidents" {
		t.Fatalf("agent description: %q", ar.Spec.Description)
	}
	if ar.Spec.Constraints == nil || ar.Spec.Constraints.MaxIterations != 8 || ar.Spec.Constraints.TimeoutSeconds != 120 {
		t.Fatalf("agent constraints: %+v", ar.Spec.Constraints)
	}
	if res.Workflows[0].Spec.Description != "triage an alert" {
		t.Fatalf("workflow description: %q", res.Workflows[0].Spec.Description)
	}
}
