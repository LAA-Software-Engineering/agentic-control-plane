package lower

import (
	"github.com/Terfyn/terfyn/internal/lang"
	"github.com/Terfyn/terfyn/internal/spec"
)

// Inline tool/policy lowering (ADR 005, issue #333): a `.agent` tool/policy declaration lowers to
// the same spec.ToolResource / spec.PolicyResource the YAML loader produces, so there is one IR and
// one validator. Presence-sensitive semantics are preserved — most importantly ToolSpec
// .OperationsDeclared, which the closed-world capability manifest (#204) derives from the presence
// of an `operations` block (including an empty one).

func (l *lowerer) tool(d *lang.ToolDecl) *spec.ToolResource {
	name := identName(d.Name)
	if name == "" {
		l.diag(d.Pos, "tool declaration has no name")
		return nil
	}
	tr := &spec.ToolResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindTool,
		Metadata:   spec.Metadata{Name: name},
		Pos:        d.Pos,
	}
	if d.Type != nil {
		tr.Spec.Type = d.Type.Name
	}
	if d.Safety != nil {
		tr.Spec.Safety = &spec.ToolSafety{
			Trusted:          d.Safety.Trusted,
			SideEffects:      d.Safety.SideEffects,
			RequiresApproval: d.Safety.RequiresApproval,
		}
	}
	if d.Operations != nil {
		// Presence of the block — even `operations {}` — is a closed callable world. This mirrors the
		// YAML `operations:` key stamping and is the security boundary the two front ends must agree on.
		tr.Spec.OperationsDeclared = true
		tr.Spec.Operations = map[string]spec.ToolOperation{}
		for _, op := range d.Operations.Ops {
			opName := identName(op.Name)
			if opName == "" {
				continue
			}
			var effs []string
			for _, e := range op.Effects {
				if e != nil {
					effs = append(effs, e.Name)
				}
			}
			tr.Spec.Operations[opName] = spec.ToolOperation{Effects: effs}
		}
	}
	return tr
}

func (l *lowerer) policy(d *lang.PolicyDecl) *spec.PolicyResource {
	name := identName(d.Name)
	if name == "" {
		l.diag(d.Pos, "policy declaration has no name")
		return nil
	}
	pr := &spec.PolicyResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindPolicy,
		Metadata:   spec.Metadata{Name: name},
		Pos:        d.Pos,
	}
	if preset := identName(d.Preset); preset != "" {
		pr.Spec.Preset = preset
	}
	if e := d.Execution; e != nil {
		ex := &spec.PolicyExecution{}
		if e.MaxTotalCostUsd != nil {
			ex.MaxTotalCostUsd = *e.MaxTotalCostUsd
		}
		if e.MaxWallClockSeconds != nil {
			ex.MaxWallClockSeconds = *e.MaxWallClockSeconds
		}
		if e.RequireStructuredOutput != nil {
			ex.RequireStructuredOutput = *e.RequireStructuredOutput
		}
		pr.Spec.Execution = ex
	}
	if a := d.Approvals; a != nil {
		ap := &spec.PolicyApprovals{RequireAllTools: a.RequireAllTools, Permissive: a.Permissive}
		for _, g := range a.RequiredFor {
			if uses := grantUses(g); uses != "" {
				ap.RequiredFor = append(ap.RequiredFor, uses)
			}
		}
		pr.Spec.Approvals = ap
	}
	if e := d.Effects; e != nil {
		ef := &spec.PolicyEffects{}
		for _, r := range e.Permit {
			if r != nil {
				ef.Permit = append(ef.Permit, r.Name)
			}
		}
		for _, r := range e.PermitWithApproval {
			if r != nil {
				ef.PermitWithApproval = append(ef.PermitWithApproval, r.Name)
			}
		}
		pr.Spec.Effects = ef
	}
	return pr
}
