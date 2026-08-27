package project

import (
	"github.com/LAA-Software-Engineering/terfyn/internal/lang/lower"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

// RefIndex summarizes symbolic references between resources (see [spec.RefIndex]).
type RefIndex = spec.RefIndex

// BuildRefIndex scans ProjectGraph resources and builds RefIndex lookup tables.
func BuildRefIndex(g *spec.ProjectGraph) *spec.RefIndex {
	return spec.BuildRefIndex(g)
}

// MergeLowered folds a lowered .agent file's resource projection into g. The
// implementation lives in internal/lang/lower so the checker and this package
// can share it without an import cycle; this is a thin, stable re-export.
func MergeLowered(g *spec.ProjectGraph, r *lower.Result) error {
	return lower.MergeLowered(g, r)
}
