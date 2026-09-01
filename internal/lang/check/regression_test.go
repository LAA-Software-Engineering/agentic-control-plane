package check

import (
	"testing"

	"github.com/Terfyn/terfyn/internal/lang"
)

// TestCheck_CrossFileWorkflowCallee_FailOpenBug is the exact repro from the
// #198 code review: a workflow callee declared only in Options.Files was
// classified as a workflow: step (so lowering shaped the call correctly) but
// its body was never itself lowered or merged into the graph Compute walked.
// effects.Compute's walkWorkflow no-ops on a missing resource, so B's real
// effects silently never entered A's bound — a fail-OPEN hole directly
// contradicting the declared clause. Check must now lower every file in the
// compilation unit, not just f.
func TestCheck_CrossFileWorkflowCallee_FailOpenBug(t *testing.T) {
	t.Parallel()
	a, diags := lang.Parse("a.agent", `
workflow A()
    effects { github.read }
{
    x = B()
}
`)
	if diags.HasErrors() {
		t.Fatalf("parse a: %v", diags)
	}
	b, diags := lang.Parse("b.agent", `
workflow B() {
    github.post_comment()
}
`)
	if diags.HasErrors() {
		t.Fatalf("parse b: %v", diags)
	}

	_, checkDiags := Check(a, Options{Files: []*lang.File{b}, Project: projectWith(githubTool())})
	if !checkDiags.HasErrors() {
		t.Fatalf("expected an error: B's github.write/external.visible must enter A's bound, got %v", diagMessages(checkDiags))
	}
	if !hasSeverity(checkDiags, lang.SeverityError, "github.write") && !hasSeverity(checkDiags, lang.SeverityError, "external.visible") {
		t.Fatalf("expected an error naming an effect only B's body reaches, got %v", diagMessages(checkDiags))
	}
}

// TestCheck_CrossFileAgentCallee_ResolvesRealGrants is the other half of the
// same repro: an agent declared only in Options.Files was not merged into the
// graph either, so effects.Compute's walkAgent fell through to its
// resource-not-found branch and reported Unknown — fail-closed, but for the
// wrong reason (a real, fully-declared grant set reported as if it were
// undeclared). A workflow whose clause exactly covers that agent's real
// grants must pass once the agent is actually merged and resolved.
func TestCheck_CrossFileAgentCallee_ResolvesRealGrants(t *testing.T) {
	t.Parallel()
	a, diags := lang.Parse("a.agent", `
workflow A()
    effects { destructive, github.write }
{
    C()
}
`)
	if diags.HasErrors() {
		t.Fatalf("parse a: %v", diags)
	}
	b, diags := lang.Parse("b.agent", `
agent C {
    grants {
        tool.github.merge_pr
    }
}
`)
	if diags.HasErrors() {
		t.Fatalf("parse b: %v", diags)
	}

	_, checkDiags := Check(a, Options{Files: []*lang.File{b}, Project: projectWith(githubTool())})
	if checkDiags.HasErrors() {
		t.Fatalf("expected no errors once C's real grants (github.write, destructive) are resolved and covered by the clause, got %v", diagMessages(checkDiags))
	}
}

// TestResolveTypes_CompileErrorIsReportedNotSwallowed is the #198 review
// finding that a schema file that EXISTS but fails to compile was treated
// identically to a missing file (silently untyped). Only a missing file is
// gradual typing (#193); a broken one must surface.
func TestResolveTypes_CompileErrorIsReportedNotSwallowed(t *testing.T) {
	t.Parallel()
	f := parseOrFatal(t, `
agent A {
    input  Broken
}
`)
	_, diags := resolveTypes(f, Options{SchemaDir: "testdata"})
	if !diags.HasErrors() {
		t.Fatalf("expected an error for a schema file that exists but fails to compile, got %v", diags)
	}
	if !hasSeverity(diags, lang.SeverityError, "Broken") {
		t.Fatalf("expected the error to name the broken type, got %v", diagMessages(diags))
	}
}

// TestCheckWorkflowArgs_MixedNamedAndPositional is the #198 review finding
// that a positional argument was always checked against ParamOrder[i] by raw
// index, ignoring which parameter slots a named argument earlier in the same
// call had already claimed. Sub(a: note, input) with Sub(a: Count, b:
// PullRequest) must check the positional `input` against b (the only unclaimed
// slot), not re-check it against a.
func TestCheckWorkflowArgs_MixedNamedAndPositional(t *testing.T) {
	t.Parallel()

	t.Run("correct binding produces no false positive", func(t *testing.T) {
		t.Parallel()
		f := parseOrFatal(t, `
workflow Sub(a: Count, b: PullRequest) -> Review
{
    github.get_pr()
}

workflow W(input: PullRequest, note: Count) -> Review
{
    x = Sub(a: note, input)
    return x
}
`)
		_, diags := Check(f, Options{Project: projectWith(githubTool()), SchemaDir: "testdata"})
		if diags.HasErrors() {
			t.Fatalf("expected no errors: the positional arg correctly binds to b, got %v", diagMessages(diags))
		}
	})

	t.Run("mismatch on the correct remaining slot is attributed to it", func(t *testing.T) {
		t.Parallel()
		f := parseOrFatal(t, `
workflow Sub(a: Count, b: PullRequest) -> Review
{
    github.get_pr()
}

workflow W(input: PullRequest, note: Count) -> Review
{
    x = Sub(a: note, input.number)
    return x
}
`)
		_, diags := Check(f, Options{Project: projectWith(githubTool()), SchemaDir: "testdata"})
		if !diags.HasErrors() {
			t.Fatalf("expected an error: input.number (integer) does not satisfy b (PullRequest), got %v", diags)
		}
		if !hasSeverity(diags, lang.SeverityError, `"b"`) {
			t.Fatalf(`expected the error to name argument "b" (the actually-unbound slot), got %v`, diagMessages(diags))
		}
		if hasSeverity(diags, lang.SeverityError, `"a"`) {
			t.Fatalf(`did not expect a false error against "a" (already bound by name), got %v`, diagMessages(diags))
		}
	})
}

// TestCheckAgentArgs_MultiArgumentIsLoudNotSilent is the #198 review finding
// that a multi-argument agent call (the ADR 002 normative surface's own
// Synthesizer(security, quality, tests) shape) was silently unchecked with no
// signal that nothing was verified. It must now emit an explicit warning when
// the agent's input type is known, rather than passing with zero diagnostics.
func TestCheckAgentArgs_MultiArgumentIsLoudNotSilent(t *testing.T) {
	t.Parallel()
	f := parseOrFatal(t, `
agent A {
    input  ReviewRequest
    output Review
}

workflow W(input: PullRequest, note: Count) -> Review
{
    r = A(input, note)
    return r
}
`)
	_, diags := Check(f, Options{Project: projectWith(githubTool()), SchemaDir: "testdata"})
	if diags.HasErrors() {
		t.Fatalf("a multi-arg agent call with an unresolved binding must not be a hard error, got %v", diagMessages(diags))
	}
	if !hasSeverity(diags, lang.SeverityWarning, "cannot type-check") {
		t.Fatalf("expected a warning making the unchecked multi-arg call explicit, got %v", diagMessages(diags))
	}
}
