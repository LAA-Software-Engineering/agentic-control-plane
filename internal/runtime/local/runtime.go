package local

import (
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// Runtime is the MVP local workflow runner backend (issue #23): project root on disk + SQLite (or any [state.RuntimeStore]).
type Runtime struct {
	ProjectRoot  string
	Store        state.RuntimeStore
	Now          func() time.Time
	AgentVersion string
}

// NewRuntime returns a local runtime. projectRoot is the directory containing project.yaml.
func NewRuntime(projectRoot string, store state.RuntimeStore) *Runtime {
	return &Runtime{ProjectRoot: projectRoot, Store: store}
}

func (r *Runtime) now() time.Time {
	if r != nil && r.Now != nil {
		return r.Now()
	}
	return time.Now().UTC()
}

func (r *Runtime) agentVersion() string {
	if r == nil {
		return "unknown"
	}
	if v := strings.TrimSpace(r.AgentVersion); v != "" {
		return v
	}
	return "0.0.0-dev"
}

// ApplyEnvironment returns a shallow copy of g with Environment overrides applied (design doc §7.6 MVP).
// Call after spec.NormalizeProjectGraph so Project.spec.defaults are merged first; agentctl and
// [Runtime.ExecuteWorkflow] use that order before spec.ValidateProjectGraph.
func ApplyEnvironment(g *spec.ProjectGraph, envName string) (*spec.ProjectGraph, error) {
	envName = strings.TrimSpace(envName)
	if envName == "" || g == nil {
		return g, nil
	}
	env, ok := g.Environments[envName]
	if !ok || env == nil {
		return nil, fmt.Errorf("local: unknown environment %q", envName)
	}
	out := shallowCloneGraph(g)
	if env.Spec.Overrides == nil {
		return out, nil
	}
	ov := env.Spec.Overrides

	for agentName, ovr := range ov.Agents {
		ar, ok := g.Agents[agentName]
		if !ok || ar == nil {
			return nil, fmt.Errorf("local: environment %q overrides unknown agent %q", envName, agentName)
		}
		cl := *ar
		cl.Spec = ar.Spec
		mergeAgentOverride(&cl.Spec, ovr)
		out.Agents[agentName] = &cl
	}

	for policyName, ovr := range ov.Policies {
		pr, ok := g.Policies[policyName]
		if !ok || pr == nil {
			return nil, fmt.Errorf("local: environment %q overrides unknown policy %q", envName, policyName)
		}
		cl := *pr
		cl.Spec = pr.Spec
		mergePolicyOverride(&cl.Spec, ovr)
		out.Policies[policyName] = &cl
	}

	return out, nil
}

func shallowCloneGraph(g *spec.ProjectGraph) *spec.ProjectGraph {
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

func mergeAgentOverride(agentSpec *spec.AgentSpec, ovr spec.AgentOverride) {
	if ovr.Model != "" {
		agentSpec.Model = ovr.Model
	}
	if ovr.Constraints != nil {
		base := agentSpec.Constraints
		if base == nil {
			base = &spec.AgentConstraints{}
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

func mergePolicyOverride(pol *spec.PolicySpec, ovr spec.PolicyOverride) {
	if ovr.Execution == nil {
		return
	}
	base := pol.Execution
	if base == nil {
		base = &spec.PolicyExecution{}
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
