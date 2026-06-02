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
//
// When pol is nil, [PolicyEvaluator.CheckRun] and [PolicyEvaluator.CheckStep] are no-ops, but
// [PolicyEvaluator.CheckToolCall] still enforces fail-closed [spec.ToolSafety] from graph (issue #103).
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
	if p != nil {
		if err := checkKnownTool(e.graph, call.Uses, p.Tools); err != nil {
			return err
		}
		if p.Approvals != nil && spec.ApprovalPermissive(p.Approvals) {
			return nil
		}
	}
	switch {
	case p != nil && spec.ResolvedPresetName(p) == spec.PresetShellSafe:
		needApproval := shellSafeRequiresApproval(e.graph, call) || approvalRequired(call.Uses, p.Approvals)
		if needApproval {
			if actionApproved(call.Uses, call.Run.ApprovedActions) {
				return nil
			}
			return toolCallApprovalDenied(call, p)
		}
		return nil
	case requiresToolCallApproval(e.graph, p, call):
		if actionApproved(call.Uses, call.Run.ApprovedActions) {
			return nil
		}
		return toolCallApprovalDenied(call, p)
	}
	if p != nil && approvalRequired(call.Uses, p.Approvals) {
		return checkApprovalGranted(call.Uses, p.Approvals, call.Run.ApprovedActions)
	}
	return checkSafetyDerived(e.graph, call)
}

func requiresToolCallApproval(graph *spec.ProjectGraph, pol *spec.PolicySpec, call ToolCallContext) bool {
	if pol == nil || pol.Approvals == nil {
		return false
	}
	return spec.ApprovalRequireAllTools(pol.Approvals)
}

func toolCallApprovalDenied(call ToolCallContext, pol *spec.PolicySpec) error {
	extra := map[string]any{"requiredFor": call.Uses}
	if pol != nil {
		if preset := spec.ResolvedPresetName(pol); preset != "" {
			extra["preset"] = preset
		}
	}
	return denied(
		ReasonApprovalRequired,
		"policy: action requires explicit approval (--approve)",
		call.Uses,
		extra,
	)
}
