package policy

import (
	"context"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// StricterOf returns an evaluator that fails closed if either a or b denies
// (issue #194). Subworkflow steps apply both the caller's and the callee's policy.
func StricterOf(a, b PolicyEvaluator) PolicyEvaluator {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &stricterEvaluator{a: a, b: b}
}

type stricterEvaluator struct {
	a, b PolicyEvaluator
}

func (s *stricterEvaluator) CheckRun(ctx context.Context, run RunContext) error {
	if err := s.a.CheckRun(ctx, run); err != nil {
		return err
	}
	return s.b.CheckRun(ctx, run)
}

func (s *stricterEvaluator) CheckStep(ctx context.Context, step StepContext) error {
	if err := s.a.CheckStep(ctx, step); err != nil {
		return err
	}
	return s.b.CheckStep(ctx, step)
}

func (s *stricterEvaluator) CheckToolCall(ctx context.Context, call ToolCallContext) error {
	if err := s.a.CheckToolCall(ctx, call); err != nil {
		return err
	}
	return s.b.CheckToolCall(ctx, call)
}

func (s *stricterEvaluator) PolicySpec() *spec.PolicySpec {
	type specCarrier interface {
		PolicySpec() *spec.PolicySpec
	}
	var left, right *spec.PolicySpec
	if c, ok := s.a.(specCarrier); ok {
		left = c.PolicySpec()
	}
	if c, ok := s.b.(specCarrier); ok {
		right = c.PolicySpec()
	}
	return mergeStricterPolicySpec(left, right)
}

func mergeStricterPolicySpec(a, b *spec.PolicySpec) *spec.PolicySpec {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	out := *a
	if b.Execution != nil {
		if out.Execution == nil {
			cp := *b.Execution
			out.Execution = &cp
		} else {
			ex := *out.Execution
			if b.Execution.MaxTotalCostUsd > 0 && (ex.MaxTotalCostUsd <= 0 || b.Execution.MaxTotalCostUsd < ex.MaxTotalCostUsd) {
				ex.MaxTotalCostUsd = b.Execution.MaxTotalCostUsd
			}
			if b.Execution.MaxWallClockSeconds > 0 && (ex.MaxWallClockSeconds <= 0 || b.Execution.MaxWallClockSeconds < ex.MaxWallClockSeconds) {
				ex.MaxWallClockSeconds = b.Execution.MaxWallClockSeconds
			}
			if b.Execution.RequireStructuredOutput {
				ex.RequireStructuredOutput = true
			}
			out.Execution = &ex
		}
	}
	if b.Hitl != nil {
		if out.Hitl == nil {
			out.Hitl = b.Hitl
		} else {
			h := *out.Hitl
			if h.InterruptOn == nil {
				h.InterruptOn = map[string]spec.HitlInterruptValue{}
			} else {
				cp := make(map[string]spec.HitlInterruptValue, len(h.InterruptOn)+len(b.Hitl.InterruptOn))
				for k, v := range h.InterruptOn {
					cp[k] = v
				}
				h.InterruptOn = cp
			}
			for k, v := range b.Hitl.InterruptOn {
				h.InterruptOn[k] = v
			}
			out.Hitl = &h
		}
	}
	return &out
}
