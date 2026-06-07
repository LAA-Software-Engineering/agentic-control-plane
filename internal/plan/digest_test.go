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
