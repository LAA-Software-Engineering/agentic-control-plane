package project

import (
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/lang/lower"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// RefIndex summarizes symbolic references between resources (see [spec.RefIndex]).
type RefIndex = spec.RefIndex

// BuildRefIndex scans ProjectGraph resources and builds RefIndex lookup tables.
func BuildRefIndex(g *spec.ProjectGraph) *spec.RefIndex {
	return spec.BuildRefIndex(g)
}

// MergeLowered folds the resource projection of a lowered .agent file (#197) into
// g so downstream validate/plan/apply can consume it alongside YAML-ingested
// resources. The graph's maps are allocated if nil. It returns an error on a name
// collision with an existing resource of the same kind, since a project must not
// declare the same Agent or Workflow twice across ingress paths.
func MergeLowered(g *spec.ProjectGraph, r *lower.Result) error {
	if g == nil || r == nil {
		return nil
	}
	if g.Agents == nil {
		g.Agents = map[string]*spec.AgentResource{}
	}
	if g.Workflows == nil {
		g.Workflows = map[string]*spec.WorkflowResource{}
	}
	for _, a := range r.Agents {
		if _, dup := g.Agents[a.Metadata.Name]; dup {
			return fmt.Errorf("project: duplicate Agent %q from lowered .agent source", a.Metadata.Name)
		}
		g.Agents[a.Metadata.Name] = a
	}
	for _, w := range r.Workflows {
		if _, dup := g.Workflows[w.Metadata.Name]; dup {
			return fmt.Errorf("project: duplicate Workflow %q from lowered .agent source", w.Metadata.Name)
		}
		g.Workflows[w.Metadata.Name] = w
	}
	return nil
}
