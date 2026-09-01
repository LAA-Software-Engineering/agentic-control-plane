package engine

import (
	"fmt"
	"strings"

	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/spec"
)

// compiledWorkflowEvaluator builds the policy evaluator for a workflow. When pinned (a run resumed
// from its deployment snapshot, issue #207), the policy is compiled from the hydrated graph and the
// on-disk .agentic/policy-snapshot.json is never consulted — otherwise a widening apply would swap
// the run's approvals/presets/safety authority underneath it via projectRoot. When not pinned, the
// deployed on-disk snapshot is the authority (current behavior for fresh runs).
func compiledWorkflowEvaluator(projectRoot string, graph *spec.ProjectGraph, policyName string, pinned bool) (policy.PolicyEvaluator, error) {
	policyName = strings.TrimSpace(policyName)
	if policyName == "" {
		policyName = policy.DefaultPolicyName(graph)
	}
	if pinned {
		cp, err := policy.Compile(graph, policyName)
		if err != nil {
			return nil, fmt.Errorf("engine: compile pinned policy %q: %w", policyName, err)
		}
		return policy.NewCompiledEvaluator(graph, cp), nil
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
