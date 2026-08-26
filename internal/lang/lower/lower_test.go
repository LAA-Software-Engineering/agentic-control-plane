package lower_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/lang"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/lang/lower"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/project"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// lowerFixture parses and lowers a testdata/<name>.agent file, failing on any
// parse or lowering diagnostic.
func lowerFixture(t *testing.T, name string) *lower.Result {
	t.Helper()
	path := filepath.Join("testdata", name+".agent")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	f, diags := lang.Parse(name+".agent", string(src))
	if len(diags) > 0 {
		t.Fatalf("parse diagnostics: %s", diags.Error())
	}
	res, ld := lower.LowerFile(f, lower.Options{})
	if len(ld) > 0 {
		t.Fatalf("lowering diagnostics: %s", ld.Error())
	}
	return res
}

// marshalResult renders a lowered result as a deterministic multi-document YAML:
// agents then workflows, each sorted by name.
func marshalResult(t *testing.T, r *lower.Result) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	agents := append([]*spec.AgentResource(nil), r.Agents...)
	sort.Slice(agents, func(i, j int) bool { return agents[i].Metadata.Name < agents[j].Metadata.Name })
	for _, a := range agents {
		if err := enc.Encode(a); err != nil {
			t.Fatalf("encode agent: %v", err)
		}
	}
	workflows := append([]*spec.WorkflowResource(nil), r.Workflows...)
	sort.Slice(workflows, func(i, j int) bool { return workflows[i].Metadata.Name < workflows[j].Metadata.Name })
	for _, w := range workflows {
		if err := enc.Encode(w); err != nil {
			t.Fatalf("encode workflow: %v", err)
		}
	}
	_ = enc.Close()
	return buf.Bytes()
}

// TestGolden_Lower lowers each fixture and compares the marshaled resource
// projection to a hand-written YAML golden. Refresh with GO_UPDATE_GOLDEN=1.
func TestGolden_Lower(t *testing.T) {
	for _, name := range []string{"adr002", "nested_calls"} {
		t.Run(name, func(t *testing.T) {
			got := marshalResult(t, lowerFixture(t, name))
			golden := filepath.Join("testdata", name+".golden.yaml")
			if os.Getenv("GO_UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden (run with GO_UPDATE_GOLDEN=1 to create): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("lowered YAML mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
			}
		})
	}
}

// graphJSON canonicalizes a lowered result's graph to JSON for identity
// comparison (Pos fields are json:"-", so only structural identity is compared).
func graphJSON(t *testing.T, r *lower.Result) []byte {
	t.Helper()
	g := r.ToGraph()
	payload := map[string]any{"agents": g.Agents, "workflows": g.Workflows}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal graph: %v", err)
	}
	return b
}

// TestLower_StabilityIdentityNotLocation is the core invariant: reformatting a
// program — blank lines, comments, reindentation — must not perturb the resource
// graph. Identity is structural; location is diagnostic metadata only (ADR 003).
func TestLower_StabilityIdentityNotLocation(t *testing.T) {
	base := graphJSON(t, lowerFixture(t, "adr002"))
	reformatted := graphJSON(t, lowerFixture(t, "adr002_reformatted"))
	if !bytes.Equal(base, reformatted) {
		t.Errorf("resource graph changed under reformatting (identity leaked from source location)\n--- adr002 ---\n%s\n--- reformatted ---\n%s", base, reformatted)
	}
}

// fullGraph assembles the lowered ADR 002 projection plus the stub resources the
// program references but does not declare (SecurityReviewer, TestReviewer,
// Synthesizer agents; the github tool; the guarded-writes policy), so the
// reference checker has a closed world to resolve against.
func fullGraph(t *testing.T, r *lower.Result) *spec.ProjectGraph {
	t.Helper()
	g := &spec.ProjectGraph{
		Agents:    map[string]*spec.AgentResource{},
		Tools:     map[string]*spec.ToolResource{},
		Workflows: map[string]*spec.WorkflowResource{},
		Policies:  map[string]*spec.PolicyResource{},
	}
	if err := project.MergeLowered(g, r); err != nil {
		t.Fatalf("merge lowered: %v", err)
	}
	for _, name := range []string{"SecurityReviewer", "TestReviewer", "Synthesizer"} {
		g.Agents[name] = &spec.AgentResource{
			APIVersion: spec.APIVersionV0, Kind: spec.KindAgent,
			Metadata: spec.Metadata{Name: name},
			Spec:     spec.AgentSpec{Model: "openai/gpt-5"},
		}
	}
	trusted := true
	g.Tools["github"] = &spec.ToolResource{
		APIVersion: spec.APIVersionV0, Kind: spec.KindTool,
		Metadata: spec.Metadata{Name: "github"},
		Spec: spec.ToolSpec{
			Type:   "mcp",
			MCP:    &spec.ToolMCP{Transport: "stdio", Command: "true"},
			Safety: &spec.ToolSafety{Trusted: &trusted, SideEffects: &trusted},
		},
	}
	g.Policies["guarded-writes"] = &spec.PolicyResource{
		APIVersion: spec.APIVersionV0, Kind: spec.KindPolicy,
		Metadata: spec.Metadata{Name: "guarded-writes"},
		Spec:     spec.PolicySpec{Preset: "strict"},
	}
	return g
}

// TestLower_ValidResourceGraph is ADR 002 acceptance: the target program lowers
// to a resource graph whose #197-owned structure — workflow steps, the needs
// graph, and every symbolic reference — passes existing validation cleanly.
//
// The one thing that keeps full ValidateProjectGraph from passing is a
// pre-existing IR limitation unrelated to lowering: ResolveAgentAdvertisedTools
// (issue #160 agent-loop advertising) permits one operation per tool, while the
// ADR 002 Reviewer grants two operations on tool.github. That gap is Epic F
// (#188/#204) — see TestLower_MultiOperationGrantIsKnownGap and the PR notes.
// This test asserts nothing ELSE is wrong.
func TestLower_ValidResourceGraph(t *testing.T) {
	g := fullGraph(t, lowerFixture(t, "adr002"))

	// The reference / step / needs-graph checks lowering is responsible for must
	// pass with no errors at all.
	if err := spec.ResolveReferences(g); err != nil {
		t.Fatalf("lowered ADR 002 graph failed reference/graph validation: %v", err)
	}

	// Full validation may only surface the known multi-operation-grant gap.
	for _, e := range flatten(spec.ValidateProjectGraph(g, "")) {
		if strings.Contains(e.Error(), "twice with different operations") {
			continue
		}
		t.Errorf("unexpected validation error (not the known Epic F multi-operation-grant gap): %v", e)
	}
}

// TestLower_MultiOperationGrantIsKnownGap pins the single point where the lowered
// ADR 002 program is not yet fully valid, so a future Epic F change that lifts it
// updates this test deliberately rather than silently. grants bind several
// concrete operations per tool; AgentSpec.Tools as consumed by the #160 agent
// loop does not yet represent that.
func TestLower_MultiOperationGrantIsKnownGap(t *testing.T) {
	g := fullGraph(t, lowerFixture(t, "adr002"))
	err := spec.ValidateProjectGraph(g, "")
	if err == nil || !strings.Contains(err.Error(), "twice with different operations") {
		t.Fatalf("expected the multi-operation-grant limitation; got: %v", err)
	}
}

// TestLower_PositionFidelityGrant checks that a diagnostic on the lowered IR — a
// missing tool behind a grant — reports the .agent grant position, not a
// synthesized resource name.
func TestLower_PositionFidelityGrant(t *testing.T) {
	r := lowerFixture(t, "adr002")
	g := fullGraph(t, r)
	delete(g.Tools, "github") // now every github grant/use is a dangling ref

	err := spec.ResolveReferences(g)
	if err == nil {
		t.Fatal("expected missing-ref errors after deleting the github tool")
	}

	// The Reviewer agent's grant tool.github.read_pr must underline the grant
	// line in adr002.agent, which the source map records.
	wantPos, ok := r.SourceMap.Lookup(lower.KeyAgentGrant("Reviewer", "tool.github.read_pr"))
	if !ok {
		t.Fatal("source map missing the grant entry")
	}
	var found *spec.MissingRefError
	for _, e := range flatten(err) {
		var mre *spec.MissingRefError
		if errors.As(e, &mre) &&
			mre.Referrer == (spec.ResourceID{Kind: spec.KindAgent, Name: "Reviewer"}) &&
			mre.Missing == (spec.ResourceID{Kind: spec.KindTool, Name: "github"}) {
			found = mre
			break
		}
	}
	if found == nil {
		t.Fatalf("no missing-ref error for Reviewer -> Tool/github; got: %v", err)
	}
	if found.Pos != wantPos {
		t.Errorf("grant diagnostic position = %+v, want the .agent grant line %+v", found.Pos, wantPos)
	}
	if !strings.HasSuffix(found.Pos.File, "adr002.agent") {
		t.Errorf("diagnostic file = %q, want the .agent source", found.Pos.File)
	}
}

// TestLower_PositionFidelityCallSite checks that a missing agent behind a
// workflow call reports the .agent call site.
func TestLower_PositionFidelityCallSite(t *testing.T) {
	r := lowerFixture(t, "adr002")
	g := fullGraph(t, r)
	delete(g.Agents, "SecurityReviewer")

	err := spec.ResolveReferences(g)
	if err == nil {
		t.Fatal("expected a missing-ref error after deleting SecurityReviewer")
	}
	wantPos, ok := r.SourceMap.Lookup(lower.KeyStep("PRReview", "security"))
	if !ok {
		t.Fatal("source map missing the security step")
	}
	var found *spec.MissingRefError
	for _, e := range flatten(err) {
		var mre *spec.MissingRefError
		if errors.As(e, &mre) && mre.Missing == (spec.ResourceID{Kind: spec.KindAgent, Name: "SecurityReviewer"}) {
			found = mre
			break
		}
	}
	if found == nil {
		t.Fatalf("no missing-ref error for missing SecurityReviewer; got: %v", err)
	}
	if found.Pos != wantPos {
		t.Errorf("call-site diagnostic position = %+v, want the .agent call site %+v", found.Pos, wantPos)
	}
}

// flatten unwraps an errors.Join tree into a flat slice.
func flatten(err error) []error {
	if err == nil {
		return nil
	}
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		var out []error
		for _, e := range j.Unwrap() {
			out = append(out, flatten(e)...)
		}
		return out
	}
	return []error{err}
}
