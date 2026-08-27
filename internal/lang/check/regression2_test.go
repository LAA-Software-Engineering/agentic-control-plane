package check

import (
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/lang"
)

// TestCheck_WorkflowCallRebindsPlaceholderKeys is the #198 review finding
// that lower.LowerFile's placeholder with: keys (argN) were never rewritten
// to the callee's real parameter names, so a positional workflow call
// type-checked clean while Program.Graph still carried a graph the callee
// cannot read (its real fields are "a"/"b", not "arg0"/"arg1"). Check must
// rewrite workflow: step with: keys using the same binding checkWorkflowArgs
// computes.
func TestCheck_WorkflowCallRebindsPlaceholderKeys(t *testing.T) {
	t.Parallel()
	f := parseOrFatal(t, `
workflow Sub(a: Count, b: PullRequest) -> Review
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
		t.Fatalf("expected no errors: note (Count) -> a, input (PullRequest) -> b are both compatible, got %v", diagMessages(diags))
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
		if _, ok := s.With["arg0"]; ok {
			t.Fatalf("expected arg0 to be rebound, still present in %+v", s.With)
		}
		if _, ok := s.With["arg1"]; ok {
			t.Fatalf("expected arg1 to be rebound, still present in %+v", s.With)
		}
		if got, want := s.With["a"], "${input.note}"; got != want {
			t.Fatalf(`expected with:"a" = %q (rebound from arg0), got %v (full: %+v)`, want, got, s.With)
		}
		if got, want := s.With["b"], "${input.input}"; got != want {
			t.Fatalf(`expected with:"b" = %q (rebound from arg1), got %v (full: %+v)`, want, got, s.With)
		}
	}
	if !found {
		t.Fatalf("expected step %q in workflow W, steps: %+v", "x", wf.Spec.Steps)
	}
}

func TestCheckWorkflowArgs_UnknownNamedArgument(t *testing.T) {
	t.Parallel()
	f := parseOrFatal(t, `
workflow Sub(a: Count, b: PullRequest) -> Review
{
    github.get_pr()
}

workflow W(note: Count) -> Review
{
    x = Sub(zzz: note)
    return x
}
`)
	_, diags := Check(f, Options{Project: projectWith(githubTool()), SchemaDir: "testdata"})
	if !diags.HasErrors() {
		t.Fatalf("expected an error for an unknown parameter name, got %v", diags)
	}
	if !hasSeverity(diags, lang.SeverityError, `"zzz"`) {
		t.Fatalf(`expected an error naming "zzz", got %v`, diagMessages(diags))
	}
}

func TestCheckWorkflowArgs_MissingRequiredArguments(t *testing.T) {
	t.Parallel()
	f := parseOrFatal(t, `
workflow Sub(a: Count, b: PullRequest) -> Review
{
    github.get_pr()
}

workflow W() -> Review
{
    x = Sub()
    return x
}
`)
	_, diags := Check(f, Options{Project: projectWith(githubTool()), SchemaDir: "testdata"})
	if !diags.HasErrors() {
		t.Fatalf("expected errors for two missing required parameters, got %v", diags)
	}
	if !hasSeverity(diags, lang.SeverityError, `"a"`) {
		t.Fatalf(`expected a missing-argument error naming "a", got %v`, diagMessages(diags))
	}
	if !hasSeverity(diags, lang.SeverityError, `"b"`) {
		t.Fatalf(`expected a missing-argument error naming "b", got %v`, diagMessages(diags))
	}
}

func TestCheckWorkflowArgs_ExtraPositionalArgument(t *testing.T) {
	t.Parallel()
	f := parseOrFatal(t, `
workflow Sub(a: Count, b: PullRequest) -> Review
{
    github.get_pr()
}

workflow W(input: PullRequest, note: Count) -> Review
{
    x = Sub(note, input, input)
    return x
}
`)
	_, diags := Check(f, Options{Project: projectWith(githubTool()), SchemaDir: "testdata"})
	if !diags.HasErrors() {
		t.Fatalf("expected an error for an extra positional argument, got %v", diags)
	}
	if !hasSeverity(diags, lang.SeverityError, "extra positional") {
		t.Fatalf("expected an extra-positional-argument error, got %v", diagMessages(diags))
	}
}

// TestCheckAgentArgs_NamedSingleIsLoudNotSilent is the #198 review finding
// that a named-single agent call (one key instead of the whole document) was
// treated as "not the one case we handle" and dropped with no diagnostic,
// even though it is exactly as undefined as a multi-argument call.
func TestCheckAgentArgs_NamedSingleIsLoudNotSilent(t *testing.T) {
	t.Parallel()
	f := parseOrFatal(t, `
agent A {
    input  ReviewRequest
    output Review
}

workflow W(input: PullRequest, note: Count) -> Review
{
    r = A(foo: note)
    return r
}
`)
	_, diags := Check(f, Options{Project: projectWith(githubTool()), SchemaDir: "testdata"})
	if diags.HasErrors() {
		t.Fatalf("a named-single agent call must warn, not hard-error, got %v", diagMessages(diags))
	}
	if !hasSeverity(diags, lang.SeverityWarning, "cannot type-check") {
		t.Fatalf("expected a warning making the unchecked named-single call explicit, got %v", diagMessages(diags))
	}
}

// TestCheckAgentArgs_ZeroArgumentsIsAnError is the #198 review finding that a
// zero-argument call to an agent with a known, declared input type was
// treated as gradual (no diagnostic at all), when it is a missing-required-
// value error, not an unresolved-type situation.
func TestCheckAgentArgs_ZeroArgumentsIsAnError(t *testing.T) {
	t.Parallel()
	f := parseOrFatal(t, `
agent A {
    input  ReviewRequest
    output Review
}

workflow W() -> Review
{
    r = A()
    return r
}
`)
	_, diags := Check(f, Options{Project: projectWith(githubTool()), SchemaDir: "testdata"})
	if !diags.HasErrors() {
		t.Fatalf("expected an error: A declares an input type but was called with no arguments, got %v", diags)
	}
	if !hasSeverity(diags, lang.SeverityError, "no arguments") {
		t.Fatalf("expected an error naming the missing input, got %v", diagMessages(diags))
	}
}
