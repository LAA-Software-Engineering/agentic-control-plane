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
		pol.Pos = srcPol.Pos
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
