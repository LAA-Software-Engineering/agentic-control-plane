package check

import (
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/execir"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/lang"
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
