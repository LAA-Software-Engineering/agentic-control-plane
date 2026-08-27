package check

import (
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/lang"
)

// TestCheck_FilesWorkflowsAreCheckedNotJustLowered is the #198 review finding
// that checkTypes/checkEffectsClauses walked only f, even though Program.Graph
// is the merged whole compilation unit and is documented as an executable
// projection of it. A positional workflow: call living in an Options.Files
// file (not f) type-checked clean and kept its lowered arg0/arg1 keys forever,
// because the rebind pass never looked at that file's calls at all.
func TestCheck_FilesWorkflowsAreCheckedNotJustLowered(t *testing.T) {
	t.Parallel()
	entry, diags := lang.Parse("entry.agent", `
workflow Entry(input: PullRequest, note: Count) -> Review
    effects { github.read }
{
    x = Helper(note, input)
    return x
}
`)
	if diags.HasErrors() {
		t.Fatalf("parse entry: %v", diags)
	}
	helper, diags := lang.Parse("helper.agent", `
workflow Helper(a: Count, b: PullRequest) -> Review
{
    y = Sub(a, b)
    return y
}

workflow Sub(a: Count, b: PullRequest) -> Review
{
    github.get_pr()
}
`)
	if diags.HasErrors() {
		t.Fatalf("parse helper: %v", diags)
	}

	prog, checkDiags := Check(entry, Options{
		Files:     []*lang.File{helper},
		Project:   projectWith(githubTool()),
		SchemaDir: "testdata",
	})
	if checkDiags.HasErrors() {
		t.Fatalf("expected no errors: every call in the unit is well-typed, got %v", diagMessages(checkDiags))
	}

	helperWF, ok := prog.Graph.Workflows["Helper"]
	if !ok {
		t.Fatalf("expected workflow Helper in the merged graph")
	}
	found := false
	for _, s := range helperWF.Spec.Steps {
		if s.ID != "y" {
			continue
		}
		found = true
		if _, ok := s.With["arg0"]; ok {
			t.Fatalf("expected arg0 to be rebound on Helper's call to Sub (a Files-only file), still present in %+v", s.With)
		}
		if _, ok := s.With["arg1"]; ok {
			t.Fatalf("expected arg1 to be rebound on Helper's call to Sub (a Files-only file), still present in %+v", s.With)
		}
		if _, ok := s.With["a"]; !ok {
			t.Fatalf(`expected with:"a" (rebound from arg0) on a Files-only file's call, got %+v`, s.With)
		}
		if _, ok := s.With["b"]; !ok {
			t.Fatalf(`expected with:"b" (rebound from arg1) on a Files-only file's call, got %+v`, s.With)
		}
	}
	if !found {
		t.Fatalf("expected step %q in workflow Helper, steps: %+v", "y", helperWF.Spec.Steps)
	}
}

// TestApplyRebinds_AliasingChainDoesNotCorruptValues is the #198 review
// finding that applyRebinds mutated a step's with: map key-by-key
// (delete(oldKey); With[newKey]=v) against a single live map, which corrupts
// data whenever one rename's target collides with a DIFFERENT rename's
// source — legal whenever a real parameter happens to be named "argN".
// Sub(arg1: Count, a: PullRequest) called as Sub(note, input) produces
// renames {arg0->arg1, arg1->a}; applying them in sequence against one map
// silently drops the first value once the second rename fires. Building a
// fresh map from the ORIGINAL entries (see applyRebinds) must not have this
// hazard.
func TestApplyRebinds_AliasingChainDoesNotCorruptValues(t *testing.T) {
	t.Parallel()
	f := parseOrFatal(t, `
workflow Sub(arg1: Count, a: PullRequest) -> Review
{
    github.get_pr()
}

workflow W(input: PullRequest, note: Count) -> Review
{
    x = Sub(note, input)
    return x
}
`)
	prog, diags := Check(f, Options{Project: projectWith(githubTool()), SchemaDir: "testdata"})
	if diags.HasErrors() {
		t.Fatalf("expected no errors, got %v", diagMessages(diags))
	}

	wf, ok := prog.Graph.Workflows["W"]
	if !ok {
		t.Fatalf("expected workflow W in the graph")
	}
	found := false
	for _, s := range wf.Spec.Steps {
		if s.ID != "x" {
			continue
		}
		found = true
		if len(s.With) != 2 {
			t.Fatalf("expected exactly 2 entries (no value dropped by the aliasing chain), got %+v", s.With)
		}
		if got, want := s.With["arg1"], "${input.note}"; got != want {
			t.Fatalf(`expected with:"arg1" = %q (the first argument, bound to the parameter literally named "arg1"), got %v (full: %+v)`, want, got, s.With)
		}
		if got, want := s.With["a"], "${input.input}"; got != want {
			t.Fatalf(`expected with:"a" = %q (the second argument), got %v (full: %+v)`, want, got, s.With)
		}
	}
	if !found {
		t.Fatalf("expected step %q in workflow W, steps: %+v", "x", wf.Spec.Steps)
	}
}
