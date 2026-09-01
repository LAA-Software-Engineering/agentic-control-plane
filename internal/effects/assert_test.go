package effects

import (
	"testing"

	"github.com/Terfyn/terfyn/internal/spec"
)

func wsTools() map[string]*spec.ToolResource {
	return map[string]*spec.ToolResource{
		"ws": {
			Metadata: spec.Metadata{Name: "ws"},
			Spec: spec.ToolSpec{Type: "native", Operations: map[string]spec.ToolOperation{
				"read_file":  {Effects: []string{"workspace.read"}},
				"write_file": {Effects: []string{"workspace.write"}},
			}},
		},
	}
}

func TestCapabilityAssertions_ForbidAndAutonomous(t *testing.T) {
	agents := map[string]*spec.AgentResource{
		"Writer": {Metadata: spec.Metadata{Name: "Writer"}, Spec: spec.AgentSpec{Tools: []string{"tool.ws.read_file", "tool.ws.write_file"}}},
		"Reader": {Metadata: spec.Metadata{Name: "Reader"}, Spec: spec.AgentSpec{Tools: []string{"tool.ws.read_file"}}},
	}
	g := graph(wsTools(), agents, nil)

	cases := []struct {
		name string
		a    CapabilityAssertions
		want int // violations
	}{
		{"reader cannot write (forbid holds)", CapabilityAssertions{ForbidEffect: []RootEffect{{"Reader", "workspace.write"}}}, 0},
		{"writer can write (forbid violated)", CapabilityAssertions{ForbidEffect: []RootEffect{{"Writer", "workspace.write"}}}, 1},
		{"writer autonomously writes (holds)", CapabilityAssertions{ExpectAutonomous: []RootEffect{{"Writer", "workspace.write"}}}, 0},
		{"reader cannot write autonomously (violated)", CapabilityAssertions{ExpectAutonomous: []RootEffect{{"Reader", "workspace.write"}}}, 1},
		{"unknown root (violated)", CapabilityAssertions{ForbidEffect: []RootEffect{{"Ghost", "workspace.write"}}}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if vs := tc.a.Evaluate(g); len(vs) != tc.want {
				t.Fatalf("got %d violations, want %d: %+v", len(vs), tc.want, vs)
			}
		})
	}
}

// TestCapabilityAssertions_StaticIsNotAutonomous: an effect reachable only via a static workflow
// uses: step is present in the bound but not autonomous, so expectAutonomous is violated.
func TestCapabilityAssertions_StaticIsNotAutonomous(t *testing.T) {
	wf := workflow("W", stepUses("s", "tool.ws.write_file"))
	g := graph(wsTools(), nil, wf)

	if vs := (CapabilityAssertions{ForbidEffect: []RootEffect{{"W", "workspace.write"}}}).Evaluate(g); len(vs) != 1 {
		t.Fatalf("W reaches write statically; forbid should be violated: %+v", vs)
	}
	if vs := (CapabilityAssertions{ExpectAutonomous: []RootEffect{{"W", "workspace.write"}}}).Evaluate(g); len(vs) != 1 {
		t.Fatalf("W reaches write only statically; expectAutonomous should be violated: %+v", vs)
	}
}

// TestCapabilityAssertions_UnknownEffectFailsLoud: a typo'd or nonexistent effect ident is a
// violation, never a vacuous pass — the review fix that keeps forbidEffect from failing open.
func TestCapabilityAssertions_UnknownEffectFailsLoud(t *testing.T) {
	agents := map[string]*spec.AgentResource{
		"Reader": {Metadata: spec.Metadata{Name: "Reader"}, Spec: spec.AgentSpec{Tools: []string{"tool.ws.read_file"}}},
	}
	g := graph(wsTools(), agents, nil)
	if vs := (CapabilityAssertions{ForbidEffect: []RootEffect{{"Reader", "workspace.wirte"}}}).Evaluate(g); len(vs) != 1 {
		t.Fatalf("a typo'd forbidEffect must fail loud, got %+v", vs)
	}
	if vs := (CapabilityAssertions{ExpectAutonomous: []RootEffect{{"Reader", "workspace.nope"}}}).Evaluate(g); len(vs) != 1 {
		t.Fatalf("a typo'd expectAutonomous must fail loud, got %+v", vs)
	}
}

// TestCapabilityAssertions_Hierarchical: a forbid on a namespace parent catches a reachable child,
// and a forbid on a leaf catches a reachable broad-grant parent (EffectCovers, like permit).
func TestCapabilityAssertions_Hierarchical(t *testing.T) {
	writer := map[string]*spec.AgentResource{
		"Writer": {Metadata: spec.Metadata{Name: "Writer"}, Spec: spec.AgentSpec{Tools: []string{"tool.ws.write_file"}}},
	}
	if vs := (CapabilityAssertions{ForbidEffect: []RootEffect{{"Writer", "workspace"}}}).Evaluate(graph(wsTools(), writer, nil)); len(vs) != 1 {
		t.Fatalf("forbid on parent %q must match reachable child, got %+v", "workspace", vs)
	}

	// A tool op declaring the parent effect; forbid on the documented leaf must still catch it.
	broad := map[string]*spec.ToolResource{
		"ws": {Metadata: spec.Metadata{Name: "ws"}, Spec: spec.ToolSpec{Type: "native", Operations: map[string]spec.ToolOperation{
			"do": {Effects: []string{"workspace"}},
		}}},
	}
	writerBroad := map[string]*spec.AgentResource{
		"Writer": {Metadata: spec.Metadata{Name: "Writer"}, Spec: spec.AgentSpec{Tools: []string{"tool.ws.do"}}},
	}
	if vs := (CapabilityAssertions{ForbidEffect: []RootEffect{{"Writer", "workspace.write"}}}).Evaluate(graph(broad, writerBroad, nil)); len(vs) != 1 {
		t.Fatalf("forbid on leaf must match a reachable broad-grant parent, got %+v", vs)
	}
}

func TestCapabilityAssertions_Gated(t *testing.T) {
	tools := map[string]*spec.ToolResource{
		"gh": {
			Metadata: spec.Metadata{Name: "gh"},
			// trusted so the fail-closed default does not mark every op as requiring approval;
			// then post is gated only via the policy's requiredFor and fetch is not gated.
			Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{Trusted: spec.BoolPtr(true)}, Operations: map[string]spec.ToolOperation{
				"post":  {Effects: []string{"github.write"}},
				"fetch": {Effects: []string{"github.read"}},
			}},
		},
	}
	wf := map[string]*spec.WorkflowResource{
		"W": {Metadata: spec.Metadata{Name: "W"}, Spec: spec.WorkflowSpec{
			Policy: "gated",
			Steps:  []spec.WorkflowStep{stepUses("a", "tool.gh.post"), stepUses("b", "tool.gh.fetch")},
		}},
	}
	g := &spec.ProjectGraph{
		Tools:     tools,
		Workflows: wf,
		Policies: map[string]*spec.PolicyResource{
			"gated": {Metadata: spec.Metadata{Name: "gated"}, Spec: spec.PolicySpec{
				Approvals: &spec.PolicyApprovals{RequiredFor: []string{"tool.gh.post"}},
			}},
		},
	}

	if vs := (CapabilityAssertions{ExpectGated: []string{"tool.gh.post"}}).Evaluate(g); len(vs) != 0 {
		t.Fatalf("tool.gh.post is in requiredFor; expectGated should hold: %+v", vs)
	}
	if vs := (CapabilityAssertions{ExpectGated: []string{"tool.gh.fetch"}}).Evaluate(g); len(vs) != 1 {
		t.Fatalf("tool.gh.fetch is not gated; expectGated should be violated: %+v", vs)
	}
	if vs := (CapabilityAssertions{ExpectGated: []string{"tool.gh.missing"}}).Evaluate(g); len(vs) != 1 {
		t.Fatalf("an unreachable op cannot be gated; expectGated should be violated: %+v", vs)
	}
}
