package config

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/Terfyn/terfyn/internal/project"
	"github.com/Terfyn/terfyn/internal/spec"
)

// TestResolveGraph_convergesWithLoader is the ADR 007 step-4 guarantee: the typed ResourceGraph ingress
// and the .agent loader converge on the same canonical ResolvedConfig. Feeding the loader's own graph
// through ResolveGraph produces the identical digest, executables, and state path as Resolve — the two
// front doors share one control plane; the machine ingress is not a parallel pipeline.
func TestResolveGraph_convergesWithLoader(t *testing.T) {
	root := t.TempDir()
	writeYAML(t, filepath.Join(root, "main.agent"), `
tool helper {
    type native
    safety {
        sideEffects false
    }
}

policy default {
}

agent reviewer {
    model mock/gpt-4
    instructions "Summarize."
}

workflow demo(input: any) policy default {
    a = helper.echo(topic: input.topic)
    return { out: a }
}
`)
	// The loader path.
	rcLoad, err := Resolve(ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// The typed machine ingress, fed the SAME graph the loader built.
	g, _, err := project.LoadProjectWithExecutables(root)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	rcGraph, err := ResolveGraph(g, ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatalf("ResolveGraph: %v", err)
	}
	if rcLoad.Digest() != rcGraph.Digest() {
		t.Fatalf("ingress diverged from loader:\n loader: %s\n ingress: %s", rcLoad.Digest(), rcGraph.Digest())
	}
	if rcLoad.StatePath() != rcGraph.StatePath() {
		t.Fatalf("state path diverged: %q vs %q", rcLoad.StatePath(), rcGraph.StatePath())
	}
	if _, ok := rcGraph.Executables()["demo"]; !ok {
		t.Fatalf("machine ingress must lower the workflow to an executable, got %v", rcGraph.Executables())
	}
}

// TestResolveGraph_validationNotBypassed proves the machine ingress runs the SAME validation as the
// loader — it is a construction boundary, not an authority bypass. A graph whose workflow references a
// missing agent is rejected, exactly as a .agent project with the same dangling reference would be.
func TestResolveGraph_validationNotBypassed(t *testing.T) {
	g := &spec.ProjectGraph{
		Meta: spec.Metadata{Name: "demo"},
		Workflows: map[string]*spec.WorkflowResource{
			"w": {
				Metadata: spec.Metadata{Name: "w"},
				Spec:     spec.WorkflowSpec{Steps: []spec.WorkflowStep{{ID: "s", Agent: "missing-bot"}}},
			},
		},
	}
	_, err := ResolveGraph(g, ResolveOptions{ProjectRoot: t.TempDir()})
	if err == nil {
		t.Fatal("ResolveGraph must not bypass validation: a missing agent reference must be rejected")
	}
}

// TestResolveGraph_deterministicAndNonMutating: resolving the same graph twice yields the same digest,
// and the producer's graph is never mutated by the ingress (it is cloned before normalize/overlay).
func TestResolveGraph_deterministicAndNonMutating(t *testing.T) {
	root := t.TempDir()
	writeYAML(t, filepath.Join(root, "main.agent"), `
tool helper {
    type native
    safety {
        sideEffects false
    }
}

workflow demo(input: any) {
    a = helper.echo(topic: input.topic)
    return { out: a }
}
`)
	g, _, err := project.LoadProjectWithExecutables(root)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	before, err := spec.CloneProjectGraph(g)
	if err != nil {
		t.Fatal(err)
	}
	rc1, err := ResolveGraph(g, ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatalf("ResolveGraph: %v", err)
	}
	rc2, err := ResolveGraph(g, ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatalf("ResolveGraph (2nd): %v", err)
	}
	if rc1.Digest() != rc2.Digest() {
		t.Fatalf("non-deterministic digest: %s vs %s", rc1.Digest(), rc2.Digest())
	}
	// The producer's graph is unchanged (the ingress cloned it): its pre-resolve digest still matches.
	dBefore, err := spec.CloneProjectGraph(before)
	if err != nil {
		t.Fatal(err)
	}
	if k1, k2 := graphKey(dBefore), graphKey(g); k1 != k2 {
		t.Fatalf("ResolveGraph mutated the producer's graph:\n before: %s\n after:  %s", k1, k2)
	}
}

// graphKey is a stable structural key of a graph — resource names plus each workflow's normalize-
// sensitive fields (policy, step ids/callees) — enough to detect the ingress mutating the producer's
// copy (normalization defaults a tool's safety, applies overlays, etc.) without depending on resolver
// internals.
func graphKey(g *spec.ProjectGraph) string {
	key := g.Meta.Name + "|"
	for _, n := range sortedNames(g.Agents) {
		a := g.Agents[n]
		key += "a:" + n + ":" + a.Spec.Model + ","
	}
	for _, n := range sortedNames(g.Tools) {
		tr := g.Tools[n]
		key += "t:" + n + ":" + tr.Spec.Type + ":"
		if tr.Spec.Safety != nil {
			key += "safety"
		}
		key += ","
	}
	for _, n := range sortedNames(g.Workflows) {
		w := g.Workflows[n]
		key += "w:" + n + ":" + w.Spec.Policy + ":"
		for _, st := range w.Spec.Steps {
			key += st.ID + ">"
		}
		key += ","
	}
	return key
}

func sortedNames[T any](m map[string]T) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
