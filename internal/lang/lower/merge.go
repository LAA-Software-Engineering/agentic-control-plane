package lower

import (
	"errors"
	"fmt"

	"github.com/Terfyn/terfyn/internal/spec"
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
	if g.Tools == nil {
		g.Tools = map[string]*spec.ToolResource{}
	}
	if g.Policies == nil {
		g.Policies = map[string]*spec.PolicyResource{}
	}
	if g.Environments == nil {
		g.Environments = map[string]*spec.EnvironmentResource{}
	}

	// A duplicate (kind, name) across any ingress — an inline resource colliding with an imported
	// YAML one, or two inline declarations — is an error with no precedence (ADR 005 §3): neither
	// YAML nor .agent wins. Silent shadowing of a Policy or Tool would hide a safety-surface change.
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
	seenTool := make(map[string]bool, len(r.Tools))
	for _, t := range r.Tools {
		n := t.Metadata.Name
		if _, dup := g.Tools[n]; dup || seenTool[n] {
			errs = append(errs, fmt.Errorf("project: duplicate Tool %q from lowered .agent source (also declared in YAML or another .agent block)", n))
		}
		seenTool[n] = true
	}
	seenPolicy := make(map[string]bool, len(r.Policies))
	for _, pol := range r.Policies {
		n := pol.Metadata.Name
		if _, dup := g.Policies[n]; dup || seenPolicy[n] {
			errs = append(errs, fmt.Errorf("project: duplicate Policy %q from lowered .agent source (also declared in YAML or another .agent block)", n))
		}
		seenPolicy[n] = true
	}
	seenEnv := make(map[string]bool, len(r.Environments))
	for _, e := range r.Environments {
		n := e.Metadata.Name
		if _, dup := g.Environments[n]; dup || seenEnv[n] {
			errs = append(errs, fmt.Errorf("project: duplicate Environment %q from lowered .agent source (also declared in YAML or another .agent block)", n))
		}
		seenEnv[n] = true
	}
	// Provider aliases lower into g.Spec.Providers.Models (project config), not a resource map. A
	// duplicate alias — colliding with a YAML `providers.models` entry or another .agent `provider` —
	// is an error with no precedence, mirroring the resource kinds (ADR 005 §3): silently shadowing a
	// provider would swap a model endpoint/credential out from under the author.
	seenProvider := make(map[string]bool, len(r.Providers))
	for _, pv := range r.Providers {
		_, dupYAML := existingProviderModels(g)[pv.Name]
		if dupYAML || seenProvider[pv.Name] {
			errs = append(errs, fmt.Errorf("project: duplicate provider %q from lowered .agent source (also declared in YAML providers.models or another .agent block)", pv.Name))
		}
		seenProvider[pv.Name] = true
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
	for _, t := range r.Tools {
		g.Tools[t.Metadata.Name] = t
	}
	for _, pol := range r.Policies {
		g.Policies[pol.Metadata.Name] = pol
	}
	for _, e := range r.Environments {
		g.Environments[e.Metadata.Name] = e
	}
	for _, pv := range r.Providers {
		setProviderModel(g, pv.Name, pv.Config)
	}
	return nil
}

// existingProviderModels returns g's current provider-alias map (nil-safe), for collision checks.
func existingProviderModels(g *spec.ProjectGraph) map[string]spec.ModelProviderConfig {
	if g.Spec.Providers == nil {
		return nil
	}
	return g.Spec.Providers.Models
}
