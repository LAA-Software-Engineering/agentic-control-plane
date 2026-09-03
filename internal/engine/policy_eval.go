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
	// A project that declares no policy resolves its workflows to the implicit default name "default"
	// (policy.DefaultPolicyName). When no such policy is declared in the graph — the .agent-only /
	// no-policy case (issue #438) — run under the safety-derived evaluator (NewEvaluator with a nil
	// policy) instead of failing "unknown policy default". This is the SAME authority plan/risk and
	// the external-runtime path (agentcli) already assume for a no-policy project: the closed-world
	// capability manifest (#204) still binds as a hard boundary and tool safety metadata still gates
	// approvals — no policy widening. The graph is not mutated, so resolved-config digests and plan
	// risk are unchanged. An explicitly-named-but-missing policy (name != "default") still fails
	// loudly below. Whether pinned or fresh, `graph` is already the authority (hydrated snapshot when
	// pinned, current config otherwise), so deriving from it stays consistent with the run's pin.
	if policyName == policy.DefaultPolicyName(nil) && !policyDeclared(graph, policyName) {
		return policy.NewEvaluator(graph, nil), nil
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

// policyDeclared reports whether the graph declares a policy resource named name.
func policyDeclared(graph *spec.ProjectGraph, name string) bool {
	if graph == nil {
		return false
	}
	pr, ok := graph.Policies[name]
	return ok && pr != nil
}
