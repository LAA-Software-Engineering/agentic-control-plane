package lower

import (
	"errors"
	"fmt"

	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

// MergeLowered folds the resource projection of a lowered .agent file (#197) into
// g so downstream validate/plan/apply can consume it alongside YAML-ingested
// resources. The graph's maps are allocated if nil.
//
// The merge is atomic: every collision — with a resource already in g, or a
// duplicate within the Result itself — is collected before any write, and if any
// exist g is left untouched and a joined error is returned. A project must not
// declare the same Agent or Workflow twice across ingress paths, and a caller
// that ignores the error must not be left with a half-merged graph.
//
// This lives in internal/lang/lower (not internal/project) so both the checker
// (internal/lang/check) and the project loader can call it: the loader now runs
// the checker to build the executable graph, and the previous home in
// internal/project would have made check -> project -> check a cycle.
func MergeLowered(g *spec.ProjectGraph, r *Result) error {
	if g == nil || r == nil {
		return nil
	}
	if g.Agents == nil {
		g.Agents = map[string]*spec.AgentResource{}
	}
	if g.Workflows == nil {
		g.Workflows = map[string]*spec.WorkflowResource{}
	}

	var errs []error
	seenAgent := make(map[string]bool, len(r.Agents))
	for _, a := range r.Agents {
		n := a.Metadata.Name
		if _, dup := g.Agents[n]; dup || seenAgent[n] {
			errs = append(errs, fmt.Errorf("project: duplicate Agent %q from lowered .agent source", n))
		}
		seenAgent[n] = true
	}
	seenWorkflow := make(map[string]bool, len(r.Workflows))
	for _, w := range r.Workflows {
		n := w.Metadata.Name
		if _, dup := g.Workflows[n]; dup || seenWorkflow[n] {
			errs = append(errs, fmt.Errorf("project: duplicate Workflow %q from lowered .agent source", n))
		}
		seenWorkflow[n] = true
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	for _, a := range r.Agents {
		g.Agents[a.Metadata.Name] = a
	}
	for _, w := range r.Workflows {
		g.Workflows[w.Metadata.Name] = w
	}
	return nil
}
