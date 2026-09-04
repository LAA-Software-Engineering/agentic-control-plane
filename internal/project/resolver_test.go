package project

import (
	"errors"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/spec"
)

// These tests exercise ResolveReferences on the typed resource graph directly (ADR 007 step 5: IR-level
// tests do not go through the project-YAML loader). The graphs are built in-process — the same shape a
// machine producer would hand the typed ingress — so reference resolution is tested as a graph
// invariant, independent of any source frontend.

func agentRes(name string) *spec.AgentResource {
	return &spec.AgentResource{Metadata: spec.Metadata{Name: name}}
}

func toolRes(name, typ string) *spec.ToolResource {
	return &spec.ToolResource{Metadata: spec.Metadata{Name: name}, Spec: spec.ToolSpec{Type: typ}}
}

func workflowRes(name string, steps ...spec.WorkflowStep) *spec.WorkflowResource {
	return &spec.WorkflowResource{Metadata: spec.Metadata{Name: name}, Spec: spec.WorkflowSpec{Steps: steps}}
}

func graphOf(agents []*spec.AgentResource, tools []*spec.ToolResource, workflows []*spec.WorkflowResource) *spec.ProjectGraph {
	g := &spec.ProjectGraph{
		Agents:    map[string]*spec.AgentResource{},
		Tools:     map[string]*spec.ToolResource{},
		Workflows: map[string]*spec.WorkflowResource{},
		Policies:  map[string]*spec.PolicyResource{},
	}
	for _, a := range agents {
		g.Agents[a.Metadata.Name] = a
	}
	for _, tr := range tools {
		g.Tools[tr.Metadata.Name] = tr
	}
	for _, w := range workflows {
		g.Workflows[w.Metadata.Name] = w
	}
	return g
}

func TestResolveReferences_missingAgent(t *testing.T) {
	g := graphOf(nil, nil, []*spec.WorkflowResource{
		workflowRes("badwf", spec.WorkflowStep{ID: "only", Agent: "ghost", With: map[string]any{}}),
	})
	err := ResolveReferences(g)
	var mr *MissingRefError
	if !errors.As(err, &mr) {
		t.Fatalf("want *MissingRefError, got %T: %v", err, err)
	}
	if mr.Referrer != (spec.ResourceID{Kind: spec.KindWorkflow, Name: "badwf"}) {
		t.Fatalf("Referrer = %v", mr.Referrer)
	}
	if mr.Missing != (spec.ResourceID{Kind: spec.KindAgent, Name: "ghost"}) {
		t.Fatalf("Missing = %v", mr.Missing)
	}
}

func TestResolveReferences_unknownTool(t *testing.T) {
	g := graphOf(nil, []*spec.ToolResource{toolRes("github", "mcp")}, []*spec.WorkflowResource{
		workflowRes("uses-unknown", spec.WorkflowStep{ID: "call", Uses: "tool.nope.get", With: map[string]any{}}),
	})
	err := ResolveReferences(g)
	var mr *MissingRefError
	if !errors.As(err, &mr) {
		t.Fatalf("want *MissingRefError, got %T: %v", err, err)
	}
	if mr.Referrer != (spec.ResourceID{Kind: spec.KindWorkflow, Name: "uses-unknown"}) {
		t.Fatalf("Referrer = %v", mr.Referrer)
	}
	if mr.Missing != (spec.ResourceID{Kind: spec.KindTool, Name: "nope"}) {
		t.Fatalf("Missing = %v", mr.Missing)
	}
}

func TestResolveReferences_forwardRefRejected(t *testing.T) {
	// `first` interpolates `${steps.second.output}` but `second` runs after it — a forward reference.
	g := graphOf([]*spec.AgentResource{agentRes("helper")}, nil, []*spec.WorkflowResource{
		workflowRes("badwf",
			spec.WorkflowStep{ID: "first", Agent: "helper", With: map[string]any{"x": "${steps.second.output}"}},
			spec.WorkflowStep{ID: "second", Agent: "helper", With: map[string]any{}},
		),
	})
	err := ResolveReferences(g)
	if err == nil {
		t.Fatal("expected forward reference error")
	}
	if !strings.Contains(err.Error(), "forward reference") {
		t.Fatalf("expected forward reference in error: %v", err)
	}
}

func TestResolveReferences_validInterpolationOrder(t *testing.T) {
	// `second` interpolates `${steps.first.output}` and runs after `first` — a valid backward reference.
	g := graphOf([]*spec.AgentResource{agentRes("helper")}, nil, []*spec.WorkflowResource{
		workflowRes("okwf",
			spec.WorkflowStep{ID: "first", Agent: "helper", With: map[string]any{}},
			spec.WorkflowStep{ID: "second", Agent: "helper", With: map[string]any{"x": "${steps.first.output}"}},
		),
	})
	if err := ResolveReferences(g); err != nil {
		t.Fatal(err)
	}
}
