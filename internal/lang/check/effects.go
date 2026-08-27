package check

import (
	"fmt"

	"github.com/LAA-Software-Engineering/terfyn/internal/effects"
	"github.com/LAA-Software-Engineering/terfyn/internal/lang"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

// checkEffectsClauses checks every WorkflowDecl declaring an `effects { }`
// clause, across every file in the compilation unit (files is f plus every
// Options.Files entry — the same set lowered and merged onto Program.Graph;
// checking only f would leave a Files-only workflow's clause unchecked even
// though its computed bound sits in the same shared Bounds this pass reads),
// against its computed bound:
//
//   - a computed effect the clause does not cover is an error, with a witness
//     path rendered by the same effects.FormatWitness the #190 policy
//     violation message uses (AUTONOMOUS tag included);
//   - a declared effect the body cannot reach is a warning, not an error —
//     the declared clause is an asserted upper bound, and an over-broad bound
//     is not by itself a defect.
//
// A workflow with no effects clause is unchecked by this pass. This is a
// deliberate, independent product decision for this checker — it is NOT an
// analogue of effects.Check's YAML behavior: that function is fail-closed
// (a workflow whose Policy carries no permit list permits NOTHING once any
// tool in the graph declares operation effects), the opposite of
// "unaffected." Whether an .agent workflow should be required to declare an
// effects clause at all is a separate lint decision, not this pass's job.
func checkEffectsClauses(files []*lang.File, bounds effects.GraphBounds) lang.Diagnostics {
	var diags lang.Diagnostics
	for _, file := range files {
		if file == nil {
			continue
		}
		for _, d := range file.Decls {
			wd, ok := d.(*lang.WorkflowDecl)
			if !ok || wd.Effects == nil {
				continue
			}
			name := identName(wd.Name)
			bound, ok := bounds.Workflows[name]
			if !ok {
				continue
			}
			diags = append(diags, checkEffectsClause(wd, bound)...)
		}
	}
	return diags
}

func checkEffectsClause(wd *lang.WorkflowDecl, bound effects.Bound) lang.Diagnostics {
	var diags lang.Diagnostics
	name := identName(wd.Name)

	for _, eff := range bound.Effects {
		if eff.Unknown {
			diags = append(diags, lang.Diagnostic{
				Pos: wd.Pos,
				Msg: fmt.Sprintf(
					"workflow %q may perform an unknown effect; no effects clause can cover an operation with no declared effects\n\n  %s\n\n%s",
					name, eff.Message, effects.FormatWitness(eff.Witness, "", true, eff.Uses)),
			})
			continue
		}
		if coveredByClause(wd.Effects, eff.Ident) {
			continue
		}
		diags = append(diags, lang.Diagnostic{
			Pos: wd.Pos,
			Msg: fmt.Sprintf(
				"workflow %q may perform effect `%s`, which its effects clause does not declare\n\n%s",
				name, eff.Ident, effects.FormatWitness(eff.Witness, eff.Ident, false, eff.Uses)),
		})
	}

	for _, ref := range wd.Effects {
		if clauseIdentCoveredByBound(ref.Name, bound) {
			continue
		}
		diags = append(diags, lang.Diagnostic{
			Pos:      ref.Pos,
			Msg:      fmt.Sprintf("workflow %q declares effect `%s` but its body cannot reach it", name, ref.Name),
			Severity: lang.SeverityWarning,
		})
	}
	return diags
}

// coveredByClause reports whether some declared clause entry covers ident via
// spec.EffectCovers's dotted-prefix rule (declared "github" covers computed
// "github.read").
func coveredByClause(clause []*lang.EffectRef, ident string) bool {
	for _, ref := range clause {
		if spec.EffectCovers(ref.Name, ident) {
			return true
		}
	}
	return false
}

// clauseIdentCoveredByBound reports whether some computed effect in bound is
// covered by the single declared identifier (the reverse direction of
// coveredByClause — declared "github" is satisfied by a computed
// "github.read", so it is not over-broad).
func clauseIdentCoveredByBound(declared string, bound effects.Bound) bool {
	for _, eff := range bound.Effects {
		if eff.Unknown {
			continue
		}
		if spec.EffectCovers(declared, eff.Ident) {
			return true
		}
	}
	return false
}
