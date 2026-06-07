package spec

import (
	"fmt"
	"maps"
	"strings"
)

// ApplyEnvironment returns a shallow copy of g with Environment overrides applied (design doc §7.6 MVP).
// Call after [NormalizeProjectGraph] so Project.spec.defaults are merged first; agentctl and
// [Runtime.Invoke] and [Runtime.Resume] receive a resolved config snapshot from the control plane.
func ApplyEnvironment(g *ProjectGraph, envName string) (*ProjectGraph, error) {
	envName = strings.TrimSpace(envName)
	if envName == "" || g == nil {
		return g, nil
	}
	env, ok := g.Environments[envName]
	if !ok || env == nil {
		return nil, fmt.Errorf("spec: unknown environment %q", envName)
	}
	out := shallowCloneGraph(g)
	if env.Spec.Overrides == nil {
		return out, nil
	}
	ov := env.Spec.Overrides

	for agentName, ovr := range ov.Agents {
		ar, ok := g.Agents[agentName]
		if !ok || ar == nil {
			return nil, fmt.Errorf("spec: environment %q overrides unknown agent %q", envName, agentName)
		}
		cl := *ar
		cl.Spec = ar.Spec
		mergeAgentOverride(&cl.Spec, ovr)
		out.Agents[agentName] = &cl
	}

	for policyName, ovr := range ov.Policies {
		pr, ok := g.Policies[policyName]
		if !ok || pr == nil {
			return nil, fmt.Errorf("spec: environment %q overrides unknown policy %q", envName, policyName)
		}
		cl := *pr
		cl.Spec = pr.Spec
		mergePolicyOverride(&cl.Spec, ovr)
		out.Policies[policyName] = &cl
	}

	return out, nil
}

func shallowCloneGraph(g *ProjectGraph) *ProjectGraph {
	if g == nil {
		return nil
	}
	out := *g
	out.Agents = maps.Clone(g.Agents)
	out.Tools = maps.Clone(g.Tools)
	out.Workflows = maps.Clone(g.Workflows)
	out.Policies = maps.Clone(g.Policies)
	out.Environments = maps.Clone(g.Environments)
	return &out
}

func mergeAgentOverride(agentSpec *AgentSpec, ovr AgentOverride) {
	if ovr.Model != "" {
		agentSpec.Model = ovr.Model
	}
	if ovr.Constraints != nil {
		base := agentSpec.Constraints
		if base == nil {
			base = &AgentConstraints{}
		}
		merged := *base
		co := ovr.Constraints
		if co.MaxIterations != 0 {
			merged.MaxIterations = co.MaxIterations
		}
		if co.TimeoutSeconds != 0 {
			merged.TimeoutSeconds = co.TimeoutSeconds
		}
		if co.Temperature != 0 {
			merged.Temperature = co.Temperature
		}
		if co.RequireStructuredOutput {
			merged.RequireStructuredOutput = true
		}
		agentSpec.Constraints = &merged
	}
}

func mergePolicyOverride(pol *PolicySpec, ovr PolicyOverride) {
	if ovr.Execution == nil {
		return
	}
	base := pol.Execution
	if base == nil {
		base = &PolicyExecution{}
	}
	merged := *base
	pe := ovr.Execution
	if pe.MaxWallClockSeconds != 0 {
		merged.MaxWallClockSeconds = pe.MaxWallClockSeconds
	}
	if pe.MaxTotalCostUsd > 0 {
		merged.MaxTotalCostUsd = pe.MaxTotalCostUsd
	}
	if pe.RequireStructuredOutput {
		merged.RequireStructuredOutput = true
	}
	pol.Execution = &merged
}
