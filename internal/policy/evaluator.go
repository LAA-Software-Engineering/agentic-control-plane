package policy

import (
	"context"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// PolicyEvaluator decides whether run/step/tool actions are allowed (design doc section 12.2 H).
type PolicyEvaluator interface {
	CheckRun(ctx context.Context, run RunContext) error
	CheckStep(ctx context.Context, step StepContext) error
	CheckToolCall(ctx context.Context, call ToolCallContext) error
}

type evaluator struct {
	graph  *spec.ProjectGraph
	policy *spec.PolicySpec
}

// NewEvaluator returns a [PolicyEvaluator] for the given merged policy spec and project graph.
// A nil policy spec applies no limits (all checks no-op).
func NewEvaluator(graph *spec.ProjectGraph, pol *spec.PolicySpec) PolicyEvaluator {
	return &evaluator{graph: graph, policy: pol}
}

func (e *evaluator) spec() *spec.PolicySpec {
	if e == nil {
		return nil
	}
	return e.policy
}

func (e *evaluator) CheckRun(ctx context.Context, run RunContext) error {
	_ = ctx
	p := e.spec()
	if p == nil || p.Execution == nil {
		return nil
	}
	return checkExecutionBudgets(run, p.Execution)
}

func (e *evaluator) CheckStep(ctx context.Context, step StepContext) error {
	_ = ctx
	p := e.spec()
	if p == nil || p.Execution == nil {
		return nil
	}
	return checkStructuredOutputRequired(step, p.Execution)
}

func (e *evaluator) CheckToolCall(ctx context.Context, call ToolCallContext) error {
	_ = ctx
	p := e.spec()
	if p == nil {
		return nil
	}
	if err := checkKnownTool(e.graph, call.Uses, p.Tools); err != nil {
		return err
	}
	return checkApprovalGranted(call.Uses, p.Approvals, call.Run.ApprovedActions)
}
