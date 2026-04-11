package project

import "github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"

// RefIndex summarizes symbolic references between resources (see [spec.RefIndex]).
type RefIndex = spec.RefIndex

// BuildRefIndex scans ProjectGraph resources and builds RefIndex lookup tables.
func BuildRefIndex(g *spec.ProjectGraph) *spec.RefIndex {
	return spec.BuildRefIndex(g)
}
