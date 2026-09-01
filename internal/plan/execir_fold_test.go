package plan

import (
	"testing"

	"github.com/Terfyn/terfyn/internal/execir"
	"github.com/Terfyn/terfyn/internal/spec"
)

// TestDesiredRows_FoldsWorkflowExecDigest proves a workflow's plan spec_hash
// folds in its execution-IR digest (issue #260): the identity commits to the
// exact executable IR whenever a program exists, and a lowering-only change (a
// different Program under the same resource projection) is a visible plan change.
func TestDesiredRows_FoldsWorkflowExecDigest(t *testing.T) {
	t.Parallel()
	wf := &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "wf"},
		Spec: spec.WorkflowSpec{Steps: []spec.WorkflowStep{{ID: "a", Uses: "tool.t.op"}}},
	}
	g := &spec.ProjectGraph{Meta: spec.Metadata{Name: "p"}, Workflows: map[string]*spec.WorkflowResource{"wf": wf}}

	prog := &execir.Program{Workflow: "wf", Body: []execir.Node{&execir.InvokeTool{Bind: "a", Uses: "tool.t.op"}}}
	prog2 := &execir.Program{Workflow: "wf", Body: []execir.Node{&execir.InvokeTool{Bind: "a", Uses: "tool.t.other"}}}

	hashOf := func(execs map[string]*execir.Program) string {
		rows, err := desiredRows(g, execs)
		if err != nil {
			t.Fatalf("desiredRows: %v", err)
		}
		for _, r := range rows {
			if r.id.Kind == spec.KindWorkflow && r.id.Name == "wf" {
				return r.hash
			}
		}
		t.Fatalf("no workflow row")
		return ""
	}

	bare, err := WorkflowSpecHash(wf)
	if err != nil {
		t.Fatal(err)
	}
	// No program → resource-only hash (backward compatible).
	if got := hashOf(nil); got != bare {
		t.Fatalf("nil execs should give the bare resource hash, got %s want %s", got, bare)
	}
	// A program folds its digest → hash differs from the bare resource hash.
	folded := hashOf(map[string]*execir.Program{"wf": prog})
	if folded == bare {
		t.Fatalf("program digest was not folded into the workflow hash")
	}
	want, _ := WorkflowSpecHashWithExec(wf, prog.Digest())
	if folded != want {
		t.Fatalf("folded hash = %s, want %s", folded, want)
	}
	// A lowering-only change (same resource, different Program) moves the hash.
	if other := hashOf(map[string]*execir.Program{"wf": prog2}); other == folded {
		t.Fatalf("a different Program under the same resource projection must change the hash")
	}
}
