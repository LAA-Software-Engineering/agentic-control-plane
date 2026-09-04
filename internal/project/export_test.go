package project

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/spec"
)

// canonicalGraph renders the identity-bearing content of a graph (resource
// envelopes by name, plus project metadata/spec) as JSON. Source positions and
// other `json:"-"` diagnostic fields are excluded, matching ADR 003's rule that
// positions are never identity.
func canonicalGraph(t *testing.T, g *spec.ProjectGraph) string {
	t.Helper()
	type view struct {
		Meta         spec.Metadata
		Spec         spec.ProjectSpec
		Agents       map[string]*spec.AgentResource
		Tools        map[string]*spec.ToolResource
		Workflows    map[string]*spec.WorkflowResource
		Policies     map[string]*spec.PolicyResource
		Environments map[string]*spec.EnvironmentResource
	}
	v := view{
		Meta: g.Meta, Spec: g.Spec,
		Agents: g.Agents, Tools: g.Tools, Workflows: g.Workflows,
		Policies: g.Policies, Environments: g.Environments,
	}
	// Imports are a loading detail (the exported project imports the resources/
	// directory), not identity — exclude them from the comparison.
	v.Spec.Imports = nil
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestExport_RoundTripsThroughLoader is the ADR 003 / issue #200 acceptance
// criterion: .agent -> graph -> export YAML -> load -> identical graph.
func TestExport_RoundTripsThroughLoader(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "project.yaml", minimalProjectYAML)
	writeFile(t, root, "src/pr.agent", `
agent Reviewer {
    model openai/gpt-5
    grants {
        tool.github.read_pr
    }
}

workflow Review(input: PullRequest) -> Review {
    pr = github.get_pr(input.repo)
    parallel {
        sec = Reviewer(pr)
    }
    return sec
}
`)

	g1, err := LoadProjectAllowingYAML(root)
	if err != nil {
		t.Fatalf("initial load: %v", err)
	}

	out := filepath.Join(t.TempDir(), "exported")
	if err := WriteProjectDir(out, g1); err != nil {
		t.Fatalf("export: %v", err)
	}

	g2, err := LoadProjectAllowingYAML(out)
	if err != nil {
		t.Fatalf("reload exported project: %v", err)
	}

	if a, b := canonicalGraph(t, g1), canonicalGraph(t, g2); a != b {
		t.Fatalf("graph changed across export round-trip:\n before: %s\n after:  %s", a, b)
	}
}

// TestWriteProjectDir_ReExportSmallerGraphLeavesNoLeftovers proves the output
// directory is a closed set: exporting a smaller graph into a directory a larger
// graph was exported to does not leave an orphaned resource that reloads.
func TestWriteProjectDir_ReExportSmallerGraphLeavesNoLeftovers(t *testing.T) {
	base := &spec.ProjectGraph{
		Meta: spec.Metadata{Name: "demo"},
		Agents: map[string]*spec.AgentResource{
			"A": {APIVersion: spec.APIVersionV0, Kind: spec.KindAgent, Metadata: spec.Metadata{Name: "A"}},
			"B": {APIVersion: spec.APIVersionV0, Kind: spec.KindAgent, Metadata: spec.Metadata{Name: "B"}},
		},
	}
	out := filepath.Join(t.TempDir(), "out")
	if err := WriteProjectDir(out, base); err != nil {
		t.Fatalf("first export: %v", err)
	}

	smaller := &spec.ProjectGraph{
		Meta:   spec.Metadata{Name: "demo"},
		Agents: map[string]*spec.AgentResource{"A": base.Agents["A"]},
	}
	if err := WriteProjectDir(out, smaller); err != nil {
		t.Fatalf("re-export: %v", err)
	}

	g, err := LoadProjectAllowingYAML(out)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := g.Agents["B"]; ok {
		t.Fatalf("agent B leaked across a re-export of a smaller graph")
	}
	if _, ok := g.Agents["A"]; !ok {
		t.Fatalf("agent A missing after re-export")
	}
}

// TestWriteProjectDir_RefusesAgentSourceDest proves export refuses a directory
// that already holds .agent files (LoadProject would duplicate every resource).
func TestWriteProjectDir_RefusesAgentSourceDest(t *testing.T) {
	out := t.TempDir()
	writeFile(t, out, "existing.agent", "workflow W() { return }")
	g := &spec.ProjectGraph{Meta: spec.Metadata{Name: "demo"}}
	err := WriteProjectDir(out, g)
	if err == nil {
		t.Fatalf("expected export into a .agent-containing directory to be refused")
	}
	if !strings.Contains(err.Error(), ".agent") {
		t.Fatalf("expected the refusal to mention .agent sources, got: %v", err)
	}
}

// TestExportYAML_Deterministic proves the stream form is byte-stable, so a
// re-export of an unchanged graph produces no diff.
func TestExportYAML_Deterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "project.yaml", minimalProjectYAML)
	writeFile(t, root, "a.agent", `
agent A { model openai/gpt-5 }
agent B { model openai/gpt-5 }
workflow W(input: X) { return input.x }
`)
	g, err := LoadProjectAllowingYAML(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := ExportYAML(g)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExportYAML(g)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("ExportYAML is not deterministic")
	}
}
