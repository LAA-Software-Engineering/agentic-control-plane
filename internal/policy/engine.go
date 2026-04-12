package policy

import (
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// Engine binds a loaded project graph for policy resolution (design doc section 12.2 H MVP).
type Engine struct {
	Graph *spec.ProjectGraph
}

// NewEngine returns an engine with the merged project graph (tools, policies, etc.).
func NewEngine(g *spec.ProjectGraph) *Engine {
	return &Engine{Graph: g}
}

// Evaluator returns a [PolicyEvaluator] for the named Policy resource in the graph.
// If the policy is missing, returns a no-op evaluator (nil spec).
func (e *Engine) Evaluator(policyName string) PolicyEvaluator {
	if e == nil {
		return NewEvaluator(nil, nil)
	}
	pol := resolvePolicy(e.Graph, policyName)
	return NewEvaluator(e.Graph, pol)
}

// EvaluatorForSpec returns a [PolicyEvaluator] for an explicit merged [spec.PolicySpec]
// (e.g. after environment overrides). The engine's graph is still used for tool-name checks.
func (e *Engine) EvaluatorForSpec(pol *spec.PolicySpec) PolicyEvaluator {
	if e == nil {
		return NewEvaluator(nil, pol)
	}
	return NewEvaluator(e.Graph, pol)
}

func resolvePolicy(g *spec.ProjectGraph, name string) *spec.PolicySpec {
	name = strings.TrimSpace(name)
	if name == "" || g == nil || g.Policies == nil {
		return nil
	}
	pr, ok := g.Policies[name]
	if !ok || pr == nil {
		return nil
	}
	return &pr.Spec
}
