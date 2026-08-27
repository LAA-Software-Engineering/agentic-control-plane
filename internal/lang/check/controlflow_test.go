package check

import (
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/execir"
	"github.com/LAA-Software-Engineering/terfyn/internal/lang"
)

// TestCheckControlFlow_EffectSoundnessUnionOverBranches proves the ADR 002 §5
// invariant that a conditional cannot smuggle an unpermitted effect past the
// effects clause: the effect bound is the union over ALL reachable branches. The
// else-branch reaches a destructive/github.write operation the clause does not
// declare, so compilation must fail even though that branch is conditional.
func TestCheckControlFlow_EffectSoundnessUnionOverBranches(t *testing.T) {
	t.Parallel()
	src := `
workflow W(input: PR)
    effects { github.read }
{
    if input.urgent {
        github.get_pr()
    } else {
        github.merge_pr()
    }
}
`
	f := parseOrFatal(t, src)
	_, diags := Check(f, Options{Project: projectWith(githubTool())})
	if !diags.HasErrors() {
		t.Fatalf("expected the else-branch's ungranted effect to fail compilation, got %v", diagMessages(diags))
	}
	if !hasSeverity(diags, lang.SeverityError, "destructive") && !hasSeverity(diags, lang.SeverityError, "github.write") {
		t.Fatalf("expected an error naming the branch's ungranted effect, got %v", diagMessages(diags))
	}
}

// TestCheckControlFlow_ClauseCoveringAllBranchesPasses is the companion: when
// the clause covers the union of both arms, the workflow compiles clean.
func TestCheckControlFlow_ClauseCoveringAllBranchesPasses(t *testing.T) {
	t.Parallel()
	src := `
workflow W(input: PR)
    effects { github.read, github.write, destructive }
{
    if input.urgent {
        github.get_pr()
    } else {
        github.merge_pr()
    }
}
`
	f := parseOrFatal(t, src)
	_, diags := Check(f, Options{Project: projectWith(githubTool())})
	if diags.HasErrors() {
		t.Fatalf("expected no errors when the clause covers both arms, got %v", diagMessages(diags))
	}
}

// TestCheckControlFlow_LoopBodyEffectsCounted proves a loop body's effects are
// in the bound too: iterating and merging inside a `for` must be caught.
func TestCheckControlFlow_LoopBodyEffectsCounted(t *testing.T) {
	t.Parallel()
	src := `
workflow W(input: Repos)
    effects { github.read }
{
    for repo in input.repos {
        github.merge_pr()
    }
}
`
	f := parseOrFatal(t, src)
	_, diags := Check(f, Options{Project: projectWith(githubTool())})
	if !diags.HasErrors() {
		t.Fatalf("expected the loop body's ungranted effect to fail compilation, got %v", diagMessages(diags))
	}
}

// TestCheckControlFlow_CallInConditionRejected proves conditions are pure: a
// call inside a condition is a diagnostic (bind it first).
func TestCheckControlFlow_CallInConditionRejected(t *testing.T) {
	t.Parallel()
	src := `
workflow W(input: PR)
    effects { github.read }
{
    if github.get_pr() {
        github.get_pr()
    }
}
`
	f := parseOrFatal(t, src)
	_, diags := Check(f, Options{Project: projectWith(githubTool())})
	if !hasSeverity(diags, lang.SeverityError, "call is not allowed in a condition") {
		t.Fatalf("expected a call-in-condition diagnostic, got %v", diagMessages(diags))
	}
}

// TestCheckControlFlow_IfJoinIsExclusiveNotLastArmWins proves an `if` is
// type-checked as exclusive choice with a definite-assignment join, not as
// sequential mutation of one env (review finding).
func TestCheckControlFlow_IfJoinIsExclusiveNotLastArmWins(t *testing.T) {
	t.Parallel()

	// Positive control: the schema wiring really does forbid an undeclared field,
	// so the negative assertions below are not vacuously passing.
	t.Run("forbidden field on a definite type errors", func(t *testing.T) {
		t.Parallel()
		src := `
agent PRSrc { output PullRequest }
workflow W(input: PullRequest) {
    p = PRSrc()
    z = p.summary
}
`
		f := parseOrFatal(t, src)
		_, diags := Check(f, Options{SchemaDir: "testdata"})
		if !hasSeverity(diags, lang.SeverityError, "summary") {
			t.Fatalf("expected PullRequest to forbid .summary, got %v", diagMessages(diags))
		}
	})

	// Both arms bind x to DIFFERENT types. Old behavior typed x as whatever the
	// else arm last wrote (Review), so x.repo — valid for PullRequest, forbidden
	// by Review — errored. The join is a union (untyped/gradual), so no error.
	t.Run("differing arm types join to a union, not the else arm", func(t *testing.T) {
		t.Parallel()
		src := `
agent PRSrc { output PullRequest }
agent RevSrc { output Review }
workflow W(input: PullRequest) {
    if input.number {
        x = PRSrc()
    } else {
        x = RevSrc()
    }
    z = x.repo
}
`
		f := parseOrFatal(t, src)
		_, diags := Check(f, Options{SchemaDir: "testdata"})
		if diags.HasErrors() {
			t.Fatalf("a union join must not be typed as the else arm; got %v", diagMessages(diags))
		}
	})

	// A binding made only in the then arm is NOT definitely assigned, so a
	// reference to it after the `if` is a compile error — the same miss the
	// interpreter would hit on the untaken (else) path, caught at compile time.
	t.Run("then-only binding is a compile error after the if", func(t *testing.T) {
		t.Parallel()
		src := `
agent PRSrc { output PullRequest }
workflow W(input: PullRequest) {
    if input.number {
        r = PRSrc()
    }
    z = r.summary
}
`
		f := parseOrFatal(t, src)
		_, diags := Check(f, Options{SchemaDir: "testdata"})
		if !hasSeverity(diags, lang.SeverityError, `unresolved reference "r"`) {
			t.Fatalf("expected a then-only binding referenced after the if to be unresolved, got %v", diagMessages(diags))
		}
	})

	// A binding made in BOTH arms is definitely assigned and resolves after.
	t.Run("both-arm binding resolves after the if", func(t *testing.T) {
		t.Parallel()
		src := `
agent PRSrc { output PullRequest }
agent RevSrc { output Review }
workflow W(input: PullRequest) {
    if input.number {
        r = PRSrc()
    } else {
        r = RevSrc()
    }
    z = r.repo
}
`
		f := parseOrFatal(t, src)
		_, diags := Check(f, Options{SchemaDir: "testdata"})
		if diags.HasErrors() {
			t.Fatalf("a both-arm binding must resolve after the if, got %v", diagMessages(diags))
		}
	})
}

// TestCheckControlFlow_LoopVariableScope proves the checker and interpreter
// agree on loop scope: the loop variable and body-local bindings are NOT in
// scope after a sequential loop (a zero-iteration loop never binds them), so a
// later reference is a compile error — the same miss the interpreter hits on an
// empty collection, caught at compile time (review finding).
func TestCheckControlFlow_LoopVariableScope(t *testing.T) {
	t.Parallel()

	t.Run("loop variable after the loop is unresolved", func(t *testing.T) {
		t.Parallel()
		src := `
workflow W(input: Repos) {
    for x in input.repos {
        github.get_pr()
    }
    return x
}
`
		f := parseOrFatal(t, src)
		_, diags := Check(f, Options{Project: projectWith(githubTool())})
		if !hasSeverity(diags, lang.SeverityError, `unresolved reference "x"`) {
			t.Fatalf("expected the loop variable to be out of scope after the loop, got %v", diagMessages(diags))
		}
	})

	t.Run("loop-local binding after the loop is unresolved", func(t *testing.T) {
		t.Parallel()
		src := `
workflow W(input: Repos) {
    for x in input.repos {
        latest = github.get_pr()
    }
    return latest
}
`
		f := parseOrFatal(t, src)
		_, diags := Check(f, Options{Project: projectWith(githubTool())})
		if !hasSeverity(diags, lang.SeverityError, `unresolved reference "latest"`) {
			t.Fatalf("expected a loop-local binding to be out of scope after the loop, got %v", diagMessages(diags))
		}
	})

	t.Run("loop variable resolves inside the body", func(t *testing.T) {
		t.Parallel()
		src := `
workflow W(input: Repos) {
    for x in input.repos {
        github.build(x)
    }
}
`
		f := parseOrFatal(t, src)
		_, diags := Check(f, Options{Project: projectWith(githubTool())})
		if diags.HasErrors() {
			t.Fatalf("the loop variable must resolve inside the body, got %v", diagMessages(diags))
		}
	})
}

// TestCheckControlFlow_ExecRebindsPositionalWorkflowArgs proves positional
// workflow: arguments are rebound to real parameter names on the EXECUTION IR,
// not only on the resource projection — so the stored executable form binds its
// Invoker args by parameter name rather than the arg0/arg1 placeholders
// (review finding: rebind reached Graph but not Executables).
func TestCheckControlFlow_ExecRebindsPositionalWorkflowArgs(t *testing.T) {
	t.Parallel()
	src := `
workflow Sub(a: A, b: B) -> R {
    return a
}

workflow W(input: X) {
    r = Sub(input.a, input.b)
}
`
	f := parseOrFatal(t, src)
	prog, diags := Check(f, Options{})
	if diags.HasErrors() {
		t.Fatalf("unexpected errors: %v", diagMessages(diags))
	}
	ex := prog.Executables["W"]
	if ex == nil {
		t.Fatalf("expected execution IR for W")
	}
	var call *execir.InvokeWorkflow
	for _, n := range ex.Body {
		if iw, ok := n.(*execir.InvokeWorkflow); ok && iw.Workflow == "Sub" {
			call = iw
		}
	}
	if call == nil {
		t.Fatalf("expected an InvokeWorkflow for Sub, got %#v", ex.Body)
	}
	if _, ok := call.Args["a"]; !ok {
		t.Fatalf("expected positional arg rebound to parameter %q, got keys %v", "a", keysOf(call.Args))
	}
	if _, ok := call.Args["b"]; !ok {
		t.Fatalf("expected positional arg rebound to parameter %q, got keys %v", "b", keysOf(call.Args))
	}
	if _, ok := call.Args["arg0"]; ok {
		t.Fatalf("placeholder arg0 must not survive on the execution IR: %v", keysOf(call.Args))
	}
}

func keysOf(m map[string]execir.Value) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestCheckControlFlow_ExecutablesCarryControlFlow proves the checked program
// exposes the execution IR with the control-flow node the surface authored.
func TestCheckControlFlow_ExecutablesCarryControlFlow(t *testing.T) {
	t.Parallel()
	src := `
workflow W(input: PR)
{
    if input.urgent {
        github.get_pr()
    }
    for repo in input.repos {
        github.get_pr()
    }
    parallel for item in input.items {
        github.get_pr()
    }
}
`
	f := parseOrFatal(t, src)
	prog, diags := Check(f, Options{Project: projectWith(githubTool())})
	if diags.HasErrors() {
		t.Fatalf("unexpected errors: %v", diagMessages(diags))
	}
	ex := prog.Executables["W"]
	if ex == nil {
		t.Fatalf("expected an execution IR for W, got none")
	}
	var branches, loops, parallelLoops int
	for _, n := range ex.Body {
		switch v := n.(type) {
		case *execir.Branch:
			branches++
		case *execir.Loop:
			loops++
			if v.Parallel {
				parallelLoops++
			}
		}
	}
	if branches != 1 {
		t.Fatalf("expected 1 Branch node, got %d", branches)
	}
	if loops != 2 || parallelLoops != 1 {
		t.Fatalf("expected 2 Loop nodes (1 parallel), got %d loops (%d parallel)", loops, parallelLoops)
	}
}
