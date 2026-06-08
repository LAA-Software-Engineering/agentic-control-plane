package policy

import (
	"context"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
)

type compiledEvaluator struct {
	graph *spec.ProjectGraph
	cp    *CompiledPolicy
}

// NewCompiledEvaluator returns a [PolicyEvaluator] that evaluates against a compiled snapshot only.
// Dynamic residual predicates (shell_safe command classification, exact requiredFor) still run at call time.
func NewCompiledEvaluator(graph *spec.ProjectGraph, cp *CompiledPolicy) PolicyEvaluator {
	return &compiledEvaluator{graph: graph, cp: cp}
}

func (e *compiledEvaluator) PolicySpec() *spec.PolicySpec {
	if e == nil || e.cp == nil {
		return nil
	}
	return &spec.PolicySpec{
		Execution: e.cp.Residual.Execution,
		Hitl:      e.cp.Residual.Hitl,
	}
}

func (e *compiledEvaluator) CheckRun(ctx context.Context, run RunContext) error {
	_ = ctx
	if e == nil || e.cp == nil {
		return nil
	}
	return checkExecutionBudgets(run, e.cp.Residual.Execution)
}

func (e *compiledEvaluator) CheckStep(ctx context.Context, step StepContext) error {
	_ = ctx
	if e == nil || e.cp == nil {
		return nil
	}
	return checkStructuredOutputRequired(step, e.cp.Residual.Execution)
}

func (e *compiledEvaluator) CheckToolCall(ctx context.Context, call ToolCallContext) error {
	_ = ctx
	if e == nil || e.cp == nil {
		return checkSafetyDerived(e.graph, call)
	}
	res := e.cp.Residual
	if res.ForbidUnknownTools {
		if err := checkKnownTool(e.graph, call.Uses, &spec.PolicyTools{ForbidUnknownTools: true}); err != nil {
			return err
		}
	}
	if res.Permissive {
		return nil
	}
	switch {
	case res.ShellSafe:
		needApproval := shellSafeRequiresApproval(e.graph, call) || approvalRequired(call.Uses, &spec.PolicyApprovals{RequiredFor: res.RequiredForExact})
		if needApproval {
			if actionApproved(call.Uses, call.Run.ApprovedActions) {
				return nil
			}
			return toolCallApprovalDenied(call, shellSafePolicySpec(&res))
		}
		return nil
	case res.RequireAllTools:
		if actionApproved(call.Uses, call.Run.ApprovedActions) {
			return nil
		}
		return toolCallApprovalDenied(call, requireAllPolicySpec(&res))
	}
	if approvalRequired(call.Uses, &spec.PolicyApprovals{RequiredFor: res.RequiredForExact}) {
		return checkApprovalGranted(call.Uses, &spec.PolicyApprovals{RequiredFor: res.RequiredForExact}, call.Run.ApprovedActions)
	}
	return e.checkCompiledStatic(call)
}

func (e *compiledEvaluator) checkCompiledStatic(call ToolCallContext) error {
	toolName, _, err := tools.ParseUses(call.Uses)
	if err != nil {
		return denied(ReasonInvalidUses, "policy: "+err.Error(), call.Uses, nil)
	}
	td, ok := e.cp.Tools[toolName]
	if !ok {
		return checkSafetyDerived(e.graph, call)
	}
	switch td.Decision {
	case DecisionAllow:
		return nil
	case DecisionRequireApproval:
		if actionApproved(call.Uses, call.Run.ApprovedActions) {
			return nil
		}
		return denied(
			ReasonApprovalRequired,
			"policy: tool requires approval from compiled snapshot (--approve)",
			call.Uses,
			map[string]any{
				"tool":   toolName,
				"source": string(td.Source),
			},
		)
	default:
		return denied(
			ReasonDenied,
			"policy: tool denied by compiled snapshot",
			call.Uses,
			map[string]any{"tool": toolName, "source": string(td.Source)},
		)
	}
}

func shellSafePolicySpec(res *ResidualPolicy) *spec.PolicySpec {
	if res == nil {
		return &spec.PolicySpec{ResolvedPreset: spec.PresetShellSafe}
	}
	return &spec.PolicySpec{
		ResolvedPreset: spec.PresetShellSafe,
		Approvals:      &spec.PolicyApprovals{RequiredFor: res.RequiredForExact},
	}
}

func requireAllPolicySpec(res *ResidualPolicy) *spec.PolicySpec {
	if res == nil {
		return &spec.PolicySpec{ResolvedPreset: spec.PresetStrict}
	}
	return &spec.PolicySpec{ResolvedPreset: spec.PresetStrict}
}
