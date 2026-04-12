package spec

import (
	"reflect"
	"testing"
)

func TestNormalizeProjectGraph_agentGetsDefaultModel(t *testing.T) {
	g := &ProjectGraph{
		Spec: ProjectSpec{
			Defaults: &ProjectDefaults{
				Model:  "openai/gpt-4.1",
				Policy: "default",
			},
		},
		Agents: map[string]*AgentResource{
			"reviewer": {
				APIVersion: APIVersionV0,
				Kind:       KindAgent,
				Metadata:   Metadata{Name: "reviewer"},
				Spec: AgentSpec{
					// model intentionally omitted
					Description: "does things",
				},
			},
		},
	}

	NormalizeProjectGraph(g)

	got := g.Agents["reviewer"].Spec.Model
	if got != "openai/gpt-4.1" {
		t.Fatalf("Model = %q, want default openai/gpt-4.1", got)
	}
}

func TestNormalizeProjectGraph_agentGetsDefaultRuntime(t *testing.T) {
	g := &ProjectGraph{
		Spec: ProjectSpec{
			Defaults: &ProjectDefaults{Runtime: "local"},
		},
		Agents: map[string]*AgentResource{
			"a": {
				Kind:     KindAgent,
				Metadata: Metadata{Name: "a"},
				Spec:     AgentSpec{Model: "mock/x"},
			},
		},
	}
	NormalizeProjectGraph(g)
	if got := g.Agents["a"].Spec.Runtime; got != "local" {
		t.Fatalf("Runtime = %q, want local", got)
	}
}

func TestNormalizeProjectGraph_workflowGetsDefaultRuntime(t *testing.T) {
	g := &ProjectGraph{
		Spec: ProjectSpec{
			Defaults: &ProjectDefaults{Runtime: "local"},
		},
		Workflows: map[string]*WorkflowResource{
			"w": {
				Kind:     KindWorkflow,
				Metadata: Metadata{Name: "w"},
				Spec:     WorkflowSpec{Policy: "p"},
			},
		},
	}
	NormalizeProjectGraph(g)
	if got := g.Workflows["w"].Spec.Runtime; got != "local" {
		t.Fatalf("Runtime = %q, want local", got)
	}
}

func TestNormalizeProjectGraph_workflowGetsDefaultPolicy(t *testing.T) {
	g := &ProjectGraph{
		Spec: ProjectSpec{
			Defaults: &ProjectDefaults{Policy: "strict"},
		},
		Workflows: map[string]*WorkflowResource{
			"w1": {
				Kind:     KindWorkflow,
				Metadata: Metadata{Name: "w1"},
				Spec:     WorkflowSpec{Description: "x"},
			},
		},
	}
	NormalizeProjectGraph(g)
	if got := g.Workflows["w1"].Spec.Policy; got != "strict" {
		t.Fatalf("Workflow policy = %q, want strict", got)
	}
}

func TestNormalizeProjectGraph_idempotent(t *testing.T) {
	g := &ProjectGraph{
		Spec: ProjectSpec{
			Defaults: &ProjectDefaults{
				Model:  "openai/gpt-4.1",
				Policy: "default",
			},
		},
		Agents: map[string]*AgentResource{
			"a": {
				Kind:     KindAgent,
				Metadata: Metadata{Name: "a"},
				Spec:     AgentSpec{},
			},
		},
		Workflows: map[string]*WorkflowResource{
			"w": {
				Kind:     KindWorkflow,
				Metadata: Metadata{Name: "w"},
				Spec:     WorkflowSpec{},
			},
		},
	}

	NormalizeProjectGraph(g)
	afterFirst := snapshotGraph(t, g)

	NormalizeProjectGraph(g)
	afterSecond := snapshotGraph(t, g)

	if !reflect.DeepEqual(afterFirst, afterSecond) {
		t.Fatalf("second normalize changed state:\nfirst:  %#v\nsecond: %#v", afterFirst, afterSecond)
	}
}

func TestNormalizeProjectGraph_preservesExplicitRuntimeOverDefault(t *testing.T) {
	g := &ProjectGraph{
		Spec: ProjectSpec{
			Defaults: &ProjectDefaults{Runtime: "local"},
		},
		Agents: map[string]*AgentResource{
			"a": {Spec: AgentSpec{Runtime: "edge"}},
		},
	}
	NormalizeProjectGraph(g)
	if got := g.Agents["a"].Spec.Runtime; got != "edge" {
		t.Fatalf("Runtime = %q, want edge (explicit value must not be replaced by defaults)", got)
	}
}

func TestNormalizeProjectGraph_trimsWorkflowRuntimeWhenSet(t *testing.T) {
	g := &ProjectGraph{
		Spec: ProjectSpec{
			Defaults: &ProjectDefaults{Runtime: "local"},
		},
		Workflows: map[string]*WorkflowResource{
			"w": {
				Kind: KindWorkflow,
				Spec: WorkflowSpec{Runtime: "  local  "},
			},
		},
	}
	NormalizeProjectGraph(g)
	if got := g.Workflows["w"].Spec.Runtime; got != "local" {
		t.Fatalf("Runtime = %q, want trimmed local", got)
	}
}

func TestNormalizeProjectGraph_doesNotOverrideExplicitModel(t *testing.T) {
	g := &ProjectGraph{
		Spec: ProjectSpec{
			Defaults: &ProjectDefaults{Model: "openai/gpt-4.1"},
		},
		Agents: map[string]*AgentResource{
			"a": {
				Spec: AgentSpec{Model: "anthropic/claude-sonnet-4"},
			},
		},
	}
	NormalizeProjectGraph(g)
	if got := g.Agents["a"].Spec.Model; got != "anthropic/claude-sonnet-4" {
		t.Fatalf("Model = %q, want explicit value preserved", got)
	}
}

func TestNormalizeProjectGraph_trimsModelWhenSet(t *testing.T) {
	g := &ProjectGraph{
		Spec: ProjectSpec{
			Defaults: &ProjectDefaults{Model: "fallback"},
		},
		Agents: map[string]*AgentResource{
			"a": {
				Kind: KindAgent,
				Spec: AgentSpec{
					Model: "  custom/model  ",
				},
			},
		},
	}
	NormalizeProjectGraph(g)
	if got := g.Agents["a"].Spec.Model; got != "custom/model" {
		t.Fatalf("Model = %q, want trimmed custom/model", got)
	}
}

// snapshotGraph returns a deep-ish copy of fields we mutate for DeepEqual checks.
func snapshotGraph(t *testing.T, g *ProjectGraph) map[string]any {
	t.Helper()
	if g == nil {
		return nil
	}
	out := map[string]any{}
	if len(g.Agents) > 0 {
		am := make(map[string]AgentSpec, len(g.Agents))
		for k, v := range g.Agents {
			if v != nil {
				am[k] = v.Spec
			}
		}
		out["agents"] = am
	}
	if len(g.Workflows) > 0 {
		wm := make(map[string]WorkflowSpec, len(g.Workflows))
		for k, v := range g.Workflows {
			if v != nil {
				wm[k] = v.Spec
			}
		}
		out["workflows"] = wm
	}
	return out
}
