package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

const traceInterruptReasonHITL = "hitl"

// PendingHitlState is persisted in checkpoint context while awaiting operator input.
type PendingHitlState struct {
	StepID string                    `json:"stepId"`
	Uses   string                    `json:"uses"`
	With   map[string]any            `json:"with"`
	Review policy.ResolvedHitlReview `json:"review"`
}

// HitlRunOptions configures human-in-the-loop resolution for a run (issue #106).
type HitlRunOptions struct {
	AutoApprove bool
	Actor       string
	Decision    *policy.HitlDecisionInput
}

func (e *Executor) maybeInterruptForHitl(
	ctx context.Context,
	in RunInput,
	stepIndex int,
	step spec.WorkflowStep,
	with map[string]any,
	pol policy.PolicyEvaluator,
	pctx policy.RunContext,
	ictx Context,
	totalCost float64,
) (bool, error) {
	if in.Hitl.AutoApprove {
		return false, nil
	}
	polSpec := policySpecFromEvaluator(pol)
	gate, err := policy.BuildHitlGate(e.Graph, polSpec, policy.ToolCallContext{
		Run: pctx, StepID: step.ID, Uses: strings.TrimSpace(step.Uses), With: with,
	})
	if err != nil {
		return false, err
	}
	if gate == nil {
		return false, nil
	}
	if in.Hitl.Decision != nil && in.Resume {
		return false, nil
	}
	ictx.PendingHitl = &PendingHitlState{
		StepID: step.ID,
		Uses:   gate.Uses,
		With:   gate.With,
		Review: gate.Review,
	}
	if err := e.saveCheckpoint(ctx, in.RunID, stepIndex, step.ID, ictx, totalCost, state.CheckpointStatusInterrupted); err != nil {
		return false, fmt.Errorf("engine: save hitl checkpoint: %w", err)
	}
	if err := e.Store.UpdateRunStatus(ctx, in.RunID, state.RunStatusInterrupted); err != nil {
		return false, fmt.Errorf("engine: mark run interrupted: %w", err)
	}
	if e.Trace != nil {
		redacted := policy.RedactHitlArgs(gate.With, gate.Review.RedactKeys)
		_, _ = e.Trace.Append(ctx, in.RunID, step.ID, trace.EventApprovalRequested, map[string]any{
			"uses":             gate.Uses,
			"with":             redacted,
			"description":      gate.Review.Description,
			"allowedDecisions": gate.Review.AllowedDecisions,
			"allowedSwitchTo":  gate.Review.SwitchTargets,
			"stepIndex":        stepIndex,
		})
		_, _ = e.Trace.Append(ctx, in.RunID, step.ID, trace.EventRunInterrupted, map[string]any{
			"stepIndex": stepIndex, "stepId": step.ID, "reason": traceInterruptReasonHITL,
		})
	}
	return true, ErrInterrupted
}

func (e *Executor) resolvePendingHitl(
	ctx context.Context,
	in RunInput,
	step spec.WorkflowStep,
	pol policy.PolicyEvaluator,
	pctx policy.RunContext,
	pending *PendingHitlState,
) (uses string, with map[string]any, err error) {
	if pending == nil {
		return strings.TrimSpace(step.Uses), nil, nil
	}
	actor := strings.TrimSpace(in.Hitl.Actor)
	if actor == "" {
		actor = policy.DefaultHitlActor
	}
	var decision policy.HitlDecisionInput
	switch {
	case in.Hitl.AutoApprove:
		decision = policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: actor}
	case in.Hitl.Decision != nil:
		decision = *in.Hitl.Decision
		if strings.TrimSpace(decision.Actor) == "" {
			decision.Actor = actor
		}
	default:
		return "", nil, fmt.Errorf("engine: run %q awaiting hitl decision; resume with --decision or --auto-approve", in.RunID)
	}
	gate := policy.HitlGate{Uses: pending.Uses, With: pending.With, Review: pending.Review}
	uses, with, err = policy.ApplyHitlDecision(gate, decision)
	if err != nil {
		if decision.Kind == spec.HitlDecisionReject {
			if e.Trace != nil {
				_, _ = e.Trace.Append(ctx, in.RunID, step.ID, trace.EventApprovalResolved, map[string]any{
					"decision": spec.HitlDecisionReject,
					"actor":    decision.Actor,
					"uses":     pending.Uses,
				})
			}
			return "", nil, &policy.HitlRejectedError{Actor: decision.Actor, Uses: pending.Uses}
		}
		return "", nil, err
	}
	traceData := map[string]any{
		"decision":     decision.Kind,
		"actor":        decision.Actor,
		"uses":         pending.Uses,
		"resolvedUses": uses,
	}
	if decision.Kind == spec.HitlDecisionEdit {
		traceData["argsDiff"] = policy.HitlArgsDiff(pending.With, with)
	}
	if decision.Kind == spec.HitlDecisionSwitch {
		traceData["switchTarget"] = decision.SwitchTarget
	}
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, in.RunID, step.ID, trace.EventApprovalResolved, traceData)
	}
	pctx2 := pctx
	pctx2.ApprovedActions = append(append([]string(nil), pctx.ApprovedActions...), uses)
	if err := pol.CheckToolCall(ctx, policy.ToolCallContext{
		Run: pctx2, StepID: step.ID, Uses: uses, With: with,
	}); err != nil {
		return "", nil, err
	}
	return uses, with, nil
}

func (e *Executor) recordAutoApproveHitl(ctx context.Context, runID string, step spec.WorkflowStep, stepIndex int, gate policy.HitlGate, actor string) {
	if e.Trace == nil {
		return
	}
	if strings.TrimSpace(actor) == "" {
		actor = policy.DefaultHitlActor
	}
	redacted := policy.RedactHitlArgs(gate.With, gate.Review.RedactKeys)
	_, _ = e.Trace.Append(ctx, runID, step.ID, trace.EventApprovalRequested, map[string]any{
		"uses":             gate.Uses,
		"with":             redacted,
		"description":      gate.Review.Description,
		"allowedDecisions": gate.Review.AllowedDecisions,
		"allowedSwitchTo":  gate.Review.SwitchTargets,
		"stepIndex":        stepIndex,
		"auto":             true,
	})
	_, _ = e.Trace.Append(ctx, runID, step.ID, trace.EventApprovalResolved, map[string]any{
		"decision":     spec.HitlDecisionApprove,
		"actor":        actor,
		"uses":         gate.Uses,
		"resolvedUses": gate.Uses,
		"auto":         true,
	})
}

func policySpecFromEvaluator(pol policy.PolicyEvaluator) *spec.PolicySpec {
	type specCarrier interface {
		PolicySpec() *spec.PolicySpec
	}
	if c, ok := pol.(specCarrier); ok {
		return c.PolicySpec()
	}
	return nil
}
