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

	"github.com/Terfyn/terfyn/internal/lang"
	"github.com/Terfyn/terfyn/internal/lang/lower"
	"github.com/Terfyn/terfyn/internal/project"
	"github.com/Terfyn/terfyn/internal/spec"
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
	for _, name := range []string{"adr002", "nested_calls", "workflow_call"} {
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
			// Normalize line endings: git may check the golden out as CRLF on
			// Windows, while the YAML encoder always emits LF.
			if normalizeNewlines(got) != normalizeNewlines(want) {
				t.Errorf("lowered YAML mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
			}
		})
	}
}

// TestLower_SameFileWorkflowCalleeIsWorkflowStep pins the fix for the classifier
// defect: a call to a workflow declared in the same file lowers to a workflow:
// step under the default Options{}, and the lowered graph resolves cleanly (no
// invented missing-Agent error).
func TestLower_SameFileWorkflowCalleeIsWorkflowStep(t *testing.T) {
	r := lowerFixture(t, "workflow_call")
	var main *spec.WorkflowResource
	for _, w := range r.Workflows {
		if w.Metadata.Name == "Main" {
			main = w
		}
	}
	if main == nil || len(main.Spec.Steps) != 1 {
		t.Fatalf("expected one step in Main; got %+v", main)
	}
	st := main.Spec.Steps[0]
	if st.Workflow != "Util" || st.Agent != "" {
		t.Errorf("same-file workflow callee lowered wrong: workflow=%q agent=%q (want workflow=Util, agent empty)", st.Workflow, st.Agent)
	}
	// The named argument's key is the callee's input field, so the with: map is
	// the callee's actual input document (engine.runSubworkflowStep sets the child
	// input to this map) — not a positional arg0 wrapper the callee cannot read.
	if _, ok := st.With["text"]; !ok || len(st.With) != 1 {
		t.Errorf("named-arg call should lower to with={text: ...}; got %v", st.With)
	}
	// The whole graph must validate its references — the callee is a Workflow, so
	// no Agent/Util is invented.
	if err := spec.ResolveReferences(r.ToGraph()); err != nil {
		t.Fatalf("lowered same-file workflow-call graph failed reference validation: %v", err)
	}
}

// TestLower_WholeInputReferenceLowersCleanly asserts a bare reference to a
// single-parameter workflow's input lowers to ${input} with no diagnostic (#303):
// the execir path binds the whole input document to the parameter and runs it, and
// the resource projection (no longer executed post-#278) carries an inert ${input}.
func TestLower_WholeInputReferenceLowersCleanly(t *testing.T) {
	src := "workflow Echo(input: T) -> T { return input }\n"
	f, pd := lang.Parse("echo.agent", src)
	if len(pd) > 0 {
		t.Fatalf("unexpected parse diagnostics: %s", pd.Error())
	}
	if _, ld := lower.LowerFile(f, lower.Options{}); len(ld) > 0 {
		t.Errorf("whole-input reference must lower cleanly now; got: %s", ld.Error())
	}
	// A field access on the same parameter must also lower cleanly.
	f2, _ := lang.Parse("ok.agent", "workflow Echo(input: T) -> T { return input.field }\n")
	if _, ld2 := lower.LowerFile(f2, lower.Options{}); len(ld2) > 0 {
		t.Errorf("input.field must lower cleanly; got: %s", ld2.Error())
	}
	// The flagship pattern — the whole input aliased and handed to an agent — lowers
	// cleanly (#303).
	f3, _ := lang.Parse("flag.agent", "workflow W(input: S) -> S {\n    state = input\n    r = Reviewer(state)\n    return r\n}\n")
	if _, ld3 := lower.LowerFile(f3, lower.Options{}); len(ld3) > 0 {
		t.Errorf("state = input; Reviewer(state) must lower cleanly; got: %s", ld3.Error())
	}
}

// TestLower_DuplicateAndCrossKindNamesAreDiagnostics asserts LowerFile is the
// authority for resource identity: a duplicated agent/workflow name, and a name
// declared as both, are diagnostics rather than a silent last-write-wins or a
// silent agent: classification.
func TestLower_DuplicateAndCrossKindNamesAreDiagnostics(t *testing.T) {
	src := "agent Dup { model openai/gpt-5 }\n" +
		"agent Dup { model openai/gpt-4 }\n" +
		"workflow Both(input: T) -> T { return input }\n" +
		"agent Both { model openai/gpt-5 }\n"
	f, pd := lang.Parse("dup.agent", src)
	if len(pd) > 0 {
		t.Fatalf("unexpected parse diagnostics: %s", pd.Error())
	}
	_, ld := lower.LowerFile(f, lower.Options{})
	joined := ld.Error()
	if !strings.Contains(joined, `duplicate agent "Dup"`) {
		t.Errorf("expected a duplicate-agent diagnostic; got: %s", joined)
	}
	if !strings.Contains(joined, `"Both" is declared as both an agent and a workflow`) {
		t.Errorf("expected a cross-kind diagnostic for Both; got: %s", joined)
	}
}

// TestMergeLowered_CollisionIsAtomic asserts MergeLowered leaves the destination
// untouched when any name collides, rather than mutating then erroring.
func TestMergeLowered_CollisionIsAtomic(t *testing.T) {
	g := &spec.ProjectGraph{
		Agents:    map[string]*spec.AgentResource{"Foo": {Metadata: spec.Metadata{Name: "Foo"}}},
		Workflows: map[string]*spec.WorkflowResource{},
	}
	r := &lower.Result{Agents: []*spec.AgentResource{
		{Metadata: spec.Metadata{Name: "Bar"}},
		{Metadata: spec.Metadata{Name: "Foo"}},
	}}
	if err := project.MergeLowered(g, r); err == nil {
		t.Fatal("expected a duplicate-Agent error")
	}
	if _, leaked := g.Agents["Bar"]; leaked {
		t.Error("MergeLowered wrote Bar despite the Foo collision — merge was not atomic")
	}
	if len(g.Agents) != 1 {
		t.Errorf("destination graph mutated on a failed merge: %d agents, want 1", len(g.Agents))
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
// The lowered ADR 002 program now validates fully: the multi-operation-grant
// limitation that used to keep it from passing (ResolveAgentAdvertisedTools once
// permitted one operation per tool, while the ADR 002 Reviewer grants two
// operations on tool.github) was lifted in #291.
func TestLower_ValidResourceGraph(t *testing.T) {
	g := fullGraph(t, lowerFixture(t, "adr002"))

	// The reference / step / needs-graph checks lowering is responsible for must
	// pass with no errors at all.
	if err := spec.ResolveReferences(g); err != nil {
		t.Fatalf("lowered ADR 002 graph failed reference/graph validation: %v", err)
	}

	// Full validation now passes with no errors — including the multi-operation
	// Reviewer grant (#291).
	for _, e := range flatten(spec.ValidateProjectGraph(g, "")) {
		t.Errorf("unexpected validation error: %v", e)
	}
}

// TestLower_MultiOperationGrantResolved asserts the former one-operation-per-tool
// limitation is gone (#291): the ADR 002 Reviewer's two operations on tool.github
// lower and validate cleanly, and advertise as two distinct per-operation tool-defs.
func TestLower_MultiOperationGrantResolved(t *testing.T) {
	g := fullGraph(t, lowerFixture(t, "adr002"))
	if err := spec.ValidateProjectGraph(g, ""); err != nil {
		t.Fatalf("multi-operation grant should validate after #291, got: %v", err)
	}
	reviewer := g.Agents["Reviewer"]
	if reviewer == nil {
		t.Fatalf("no Reviewer agent in the lowered graph")
	}
	adv, err := spec.ResolveAgentAdvertisedTools(reviewer, g.Tools)
	if err != nil {
		t.Fatalf("advertise Reviewer tools: %v", err)
	}
	// The Reviewer grants multiple operations on one tool — each advertises separately.
	byName := map[string]string{}
	for _, a := range adv {
		byName[a.Name] = a.Uses
	}
	if len(adv) < 2 {
		t.Fatalf("expected multiple advertised operations, got %+v", adv)
	}
	for _, a := range adv {
		if !strings.HasPrefix(a.Uses, "tool.") {
			t.Fatalf("advertised uses %q is not a tool.<name>.<op> string", a.Uses)
		}
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

// normalizeNewlines collapses CRLF to LF so a Windows CRLF golden checkout
// compares equal to the encoder's LF output.
func normalizeNewlines(b []byte) string {
	return strings.ReplaceAll(string(b), "\r\n", "\n")
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
