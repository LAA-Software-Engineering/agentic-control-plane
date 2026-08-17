package spec

import (
	"encoding/json"
	"fmt"
)

// CloneProjectGraph returns a deep copy of g via JSON round-trip for snapshot isolation.
func CloneProjectGraph(g *ProjectGraph) (*ProjectGraph, error) {
	if g == nil {
		return nil, nil
	}
	raw, err := json.Marshal(g)
	if err != nil {
		return nil, fmt.Errorf("spec: clone project graph: %w", err)
	}
	var out ProjectGraph
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("spec: clone project graph: %w", err)
	}
	// Fields marked json:"-" are not round-tripped; copy derived snapshot state explicitly.
	out.Meta = g.Meta
	out.Pos = g.Pos
	preserveDerivedGraphFields(g, &out)
	return &out, nil
}

func preserveDerivedGraphFields(src, dst *ProjectGraph) {
	if src == nil || dst == nil {
		return
	}
	for name, pol := range dst.Policies {
		if pol == nil {
			continue
		}
		srcPol, ok := src.Policies[name]
		if !ok || srcPol == nil {
			continue
		}
		pol.Spec.ResolvedPreset = srcPol.Spec.ResolvedPreset
		copyPolicyDiagnosticPos(srcPol, pol)
	}
	for name, ar := range dst.Agents {
		srcAr, ok := src.Agents[name]
		if !ok || srcAr == nil || ar == nil {
			continue
		}
		ar.Pos = srcAr.Pos
		if n := len(srcAr.Spec.ToolsPos); n > 0 {
			ar.Spec.ToolsPos = append([]Pos(nil), srcAr.Spec.ToolsPos...)
		}
	}
	for name, tr := range dst.Tools {
		srcTr, ok := src.Tools[name]
		if !ok || srcTr == nil || tr == nil {
			continue
		}
		tr.Pos = srcTr.Pos
		copyToolDiagnosticPos(srcTr, tr)
	}
	for name, wr := range dst.Workflows {
		srcWr, ok := src.Workflows[name]
		if !ok || srcWr == nil || wr == nil {
			continue
		}
		wr.Pos = srcWr.Pos
		if len(wr.Spec.Steps) != len(srcWr.Spec.Steps) {
			continue
		}
		for i := range wr.Spec.Steps {
			wr.Spec.Steps[i].Pos = srcWr.Spec.Steps[i].Pos
			wr.Spec.Steps[i].UsesPos = srcWr.Spec.Steps[i].UsesPos
			wr.Spec.Steps[i].AgentPos = srcWr.Spec.Steps[i].AgentPos
		}
	}
	for name, er := range dst.Environments {
		srcEr, ok := src.Environments[name]
		if !ok || srcEr == nil || er == nil {
			continue
		}
		er.Pos = srcEr.Pos
	}
}

func copyPolicyDiagnosticPos(src, dst *PolicyResource) {
	if src == nil || dst == nil {
		return
	}
	dst.Pos = src.Pos
	if src.Spec.Approvals != nil && dst.Spec.Approvals != nil && len(src.Spec.Approvals.RequiredForPos) > 0 {
		dst.Spec.Approvals.RequiredForPos = append([]Pos(nil), src.Spec.Approvals.RequiredForPos...)
	}
	if src.Spec.Hitl != nil && dst.Spec.Hitl != nil && src.Spec.Hitl.InterruptOnPos != nil {
		dst.Spec.Hitl.InterruptOnPos = make(map[string]Pos, len(src.Spec.Hitl.InterruptOnPos))
		for k, p := range src.Spec.Hitl.InterruptOnPos {
			dst.Spec.Hitl.InterruptOnPos[k] = p
		}
	}
	if src.Spec.Effects != nil && dst.Spec.Effects != nil {
		if len(src.Spec.Effects.PermitPos) > 0 {
			dst.Spec.Effects.PermitPos = append([]Pos(nil), src.Spec.Effects.PermitPos...)
		}
		if len(src.Spec.Effects.PermitWithApprovalPos) > 0 {
			dst.Spec.Effects.PermitWithApprovalPos = append([]Pos(nil), src.Spec.Effects.PermitWithApprovalPos...)
		}
	}
}

func copyToolDiagnosticPos(src, dst *ToolResource) {
	if src == nil || dst == nil || len(src.Spec.Operations) == 0 || len(dst.Spec.Operations) == 0 {
		return
	}
	for name, dstOp := range dst.Spec.Operations {
		srcOp, ok := src.Spec.Operations[name]
		if !ok {
			continue
		}
		dstOp.Pos = srcOp.Pos
		if len(srcOp.EffectsPos) > 0 {
			dstOp.EffectsPos = append([]Pos(nil), srcOp.EffectsPos...)
		}
		dst.Spec.Operations[name] = dstOp
	}
}
