package project

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
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
	// Imports are a loading detail (the exported project imports resources.yaml),
	// not identity — exclude them from the comparison.
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
    if input.urgent {
        github.notify(pr)
    }
    return sec
}
`)

	g1, err := LoadProject(root)
	if err != nil {
		t.Fatalf("initial load: %v", err)
	}

	out := filepath.Join(t.TempDir(), "exported")
	if err := WriteProjectDir(out, g1); err != nil {
		t.Fatalf("export: %v", err)
	}

	g2, err := LoadProject(out)
	if err != nil {
		t.Fatalf("reload exported project: %v", err)
	}

	if a, b := canonicalGraph(t, g1), canonicalGraph(t, g2); a != b {
		t.Fatalf("graph changed across export round-trip:\n before: %s\n after:  %s", a, b)
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
	g, err := LoadProject(root)
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
