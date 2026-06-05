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
	}
}
