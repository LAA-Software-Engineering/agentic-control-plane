// This test lives in the external test package (check_test) because it loads a
// YAML project via internal/project, which now imports internal/lang/check (the
// loader compiles .agent through the checker). An internal `package check` test
// importing project would be a cycle.
package check_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/Terfyn/terfyn/internal/effects"
	"github.com/Terfyn/terfyn/internal/lang"
	"github.com/Terfyn/terfyn/internal/lang/check"
	"github.com/Terfyn/terfyn/internal/project"
	"github.com/Terfyn/terfyn/internal/spec"
)

// diffProjectWith builds a minimal project graph holding the given tools (a
// local copy of the internal-test helper, since this file is an external test).
func diffProjectWith(tools ...*spec.ToolResource) *spec.ProjectGraph {
	g := &spec.ProjectGraph{
		Tools:        map[string]*spec.ToolResource{},
		Agents:       map[string]*spec.AgentResource{},
		Workflows:    map[string]*spec.WorkflowResource{},
		Policies:     map[string]*spec.PolicyResource{},
		Environments: map[string]*spec.EnvironmentResource{},
	}
	for _, tr := range tools {
		g.Tools[tr.Metadata.Name] = tr
	}
	return g
}

// TestDifferential_AgentAndYAMLProduceIdenticalEffectBounds is issue #198's
// required differential test: an .agent program (testdata/differential/pr_review.agent)
// and a hand-written YAML project (testdata/differential/yaml/) that authors the
// same graph shape (same tool, same agent grant, same step ids) must produce
// identical effects.Compute bounds for the equivalent workflow.
//
// This does not test two independently-implemented effect walkers agreeing —
// there is only one, effects.Compute (see doc.go). It tests that lowering
// (#197) preserves the effect-relevant shape of a .agent program well enough
// that the two ingress paths converge on the same *spec.ProjectGraph shape,
// which is the property "the frontend and YAML paths must agree" actually
// depends on.
func TestDifferential_AgentAndYAMLProduceIdenticalEffectBounds(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("testdata", "differential", "pr_review.agent"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	f, diags := lang.Parse("pr_review.agent", string(src))
	if diags.HasErrors() {
		t.Fatalf("parse diagnostics: %s", diags.Error())
	}

	tool := &spec.ToolResource{
		Metadata: spec.Metadata{Name: "github"},
		Spec: spec.ToolSpec{
			Operations: map[string]spec.ToolOperation{
				"get_pr":   {Effects: []string{"github.read"}},
				"merge_pr": {Effects: []string{"github.write", "destructive"}},
			},
		},
	}
	agentProg, checkDiags := check.Check(f, check.Options{Project: diffProjectWith(tool)})
	if checkDiags.HasErrors() {
		t.Fatalf(".agent side reported errors: %s", checkDiags.Error())
	}

	yamlGraph, err := project.LoadProjectAllowingYAML(filepath.Join("testdata", "differential", "yaml"))
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	yamlBounds := effects.Compute(yamlGraph)

	agentBound, ok := agentProg.Bounds.Workflows["PRReview"]
	if !ok {
		t.Fatalf(".agent side has no bound for Workflow/PRReview: %+v", agentProg.Bounds.Workflows)
	}
	yamlBound, ok := yamlBounds.Workflows["PRReview"]
	if !ok {
		t.Fatalf("YAML side has no bound for Workflow/PRReview: %+v", yamlBounds.Workflows)
	}

	agentSnap := snapshotEffects(agentBound.Effects)
	yamlSnap := snapshotEffects(yamlBound.Effects)
	if !reflect.DeepEqual(agentSnap, yamlSnap) {
		t.Fatalf(".agent and YAML effect bounds differ:\n.agent: %+v\nYAML:   %+v", agentSnap, yamlSnap)
	}

	wantIdents := []string{"destructive", "github.read", "github.write"}
	gotIdents := make([]string, len(agentSnap))
	for i, s := range agentSnap {
		gotIdents[i] = s.Ident
	}
	if !reflect.DeepEqual(gotIdents, wantIdents) {
		t.Fatalf("unexpected effect set: got %v, want %v", gotIdents, wantIdents)
	}
}

// effectSnapshot is a comparable projection of effects.Effect over only its
// exported fields (occurrences is unexported bookkeeping internal to the
// effects package and not part of the bound's public shape).
type effectSnapshot struct {
	Ident   string
	Unknown bool
	Uses    string
	Witness []effects.Hop
}

func snapshotEffects(effs []effects.Effect) []effectSnapshot {
	out := make([]effectSnapshot, len(effs))
	for i, e := range effs {
		out[i] = effectSnapshot{Ident: e.Ident, Unknown: e.Unknown, Uses: e.Uses, Witness: e.Witness}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ident < out[j].Ident })
	return out
}
