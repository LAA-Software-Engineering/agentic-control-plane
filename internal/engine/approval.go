package engine

import (
	"context"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/telemetry"
)

func approvalHitlGate(step spec.WorkflowStep, with map[string]any) policy.HitlGate {
	if with == nil {
		with = map[string]any{}
	}
	decisions := []spec.HitlDecisionKind{spec.HitlDecisionApprove, spec.HitlDecisionReject}
	var allowedEdit []string
	if len(with) > 0 {
		decisions = append(decisions, spec.HitlDecisionEdit)
		for k := range with {
			k = strings.TrimSpace(k)
			if k != "" {
				allowedEdit = append(allowedEdit, k)
			}
		}
		sort.Strings(allowedEdit)
	}
	var redact []string
	if step.Approval != nil && step.Approval.Config != nil {
		redact = append([]string(nil), step.Approval.Config.RedactKeys...)
	}
	return policy.HitlGate{
		Uses: spec.ApprovalStepUses,
		With: with,
		Review: policy.ResolvedHitlReview{
			Description:      spec.ApprovalStepDescription(step),
			AllowedDecisions: decisions,
			AllowedEditArgs:  allowedEdit,
			RedactKeys:       redact,
		},
	}
}

func (e *Executor) runApprovalStep(
	ctx context.Context,
	persistCtx context.Context,
	in RunInput,
	wf *spec.WorkflowResource,
	wfPol policy.PolicyEvaluator,
	rt *dagRuntime,
	ictx Context,
	pctx policy.RunContext,
	runHandle *telemetry.RunHandle,
	i int,
	step spec.WorkflowStep,
	with map[string]any,
) (out map[string]any, pendingCleared bool, interrupted bool, err error) {
	gate := approvalHitlGate(step, with)
	pending := ictx.PendingHitl
	if pending != nil && pending.StepID != step.ID {
		pending = nil
	}
	if pending != nil {
		_, resolved, rerr := e.resolvePendingHitl(ctx, in, step, wfPol, pctx, pending)
		if rerr != nil {
			return nil, false, false, rerr
		}
		if resolved == nil {
			resolved = map[string]any{}
		}
		return resolved, true, false, nil
	}
	if in.Hitl.AutoApprove {
		e.recordAutoApproveHitl(persistCtx, in.RunID, step, i, gate, in.Hitl.Actor)
		if with == nil {
			with = map[string]any{}
		}
		return with, false, false, nil
	}
	rt.mu.Lock()
	liveTotal := rt.cost.get()
	interruptedHITL, ierr := e.interruptForHitlGate(persistCtx, in, wf, i, step, &gate, &rt.ictx, liveTotal, runHandle, PendingHitlKindApproval)
	rt.mu.Unlock()
	if interruptedHITL {
		return nil, false, true, ierr
	}
	return with, false, false, ierr
}
