package engine

import (
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func compiledWorkflowEvaluator(projectRoot string, graph *spec.ProjectGraph, policyName string) (policy.PolicyEvaluator, error) {
	policyName = strings.TrimSpace(policyName)
	if policyName == "" {
		policyName = policy.DefaultPolicyName(graph)
	}
	root := strings.TrimSpace(projectRoot)
	if root != "" {
		stored, err := policy.ReadSnapshotSet(root)
		if err != nil {
			return nil, fmt.Errorf("engine: read policy snapshot: %w", err)
		}
		if stored != nil {
			cp, err := policy.CompiledPolicyForName(root, graph, policyName)
			if err != nil {
				return nil, fmt.Errorf("engine: compiled policy %q: %w", policyName, err)
			}
			return policy.NewCompiledEvaluator(graph, cp), nil
		}
	}
	cp, err := policy.Compile(graph, policyName)
	if err != nil {
		return nil, fmt.Errorf("engine: compile policy %q: %w", policyName, err)
	}
	return policy.NewCompiledEvaluator(graph, cp), nil
}
