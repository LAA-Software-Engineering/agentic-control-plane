package policy

import (
	"context"
	"strings"

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
		return clonePolicySpec(b)
	}
	if b == nil {
		return clonePolicySpec(a)
	}
	out := clonePolicySpec(a)
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
	out.Hitl = mergeStricterHitl(out.Hitl, b.Hitl)
	return out
}

func clonePolicySpec(p *spec.PolicySpec) *spec.PolicySpec {
	if p == nil {
		return nil
	}
	out := *p
	if p.Execution != nil {
		cp := *p.Execution
		out.Execution = &cp
	}
	out.Hitl = cloneHitlPolicy(p.Hitl)
	if p.Approvals != nil {
		ap := *p.Approvals
		ap.RequiredFor = append([]string(nil), p.Approvals.RequiredFor...)
		out.Approvals = &ap
	}
	if p.Tools != nil {
		tp := *p.Tools
		out.Tools = &tp
	}
	if p.Effects != nil {
		ef := *p.Effects
		ef.Permit = append([]string(nil), p.Effects.Permit...)
		ef.PermitWithApproval = append([]string(nil), p.Effects.PermitWithApproval...)
		out.Effects = &ef
	}
	if p.Security != nil {
		sec := *p.Security
		out.Security = &sec
	}
	return &out
}

func cloneHitlPolicy(h *spec.HitlPolicy) *spec.HitlPolicy {
	if h == nil {
		return nil
	}
	out := spec.HitlPolicy{
		DescriptionPrefix: h.DescriptionPrefix,
		RedactKeys:        append([]string(nil), h.RedactKeys...),
	}
	if h.InterruptOn != nil {
		out.InterruptOn = make(map[string]spec.HitlInterruptValue, len(h.InterruptOn))
		for k, v := range h.InterruptOn {
			out.InterruptOn[k] = cloneHitlInterruptValue(v)
		}
	}
	if h.ToolSwitchMap != nil {
		out.ToolSwitchMap = cloneStringListMap(h.ToolSwitchMap)
	}
	return &out
}

func cloneHitlInterruptValue(v spec.HitlInterruptValue) spec.HitlInterruptValue {
	out := spec.HitlInterruptValue{Enabled: v.Enabled}
	if v.Config != nil {
		out.Config = cloneHitlInterruptConfig(v.Config)
	}
	return out
}

func cloneHitlInterruptConfig(c *spec.HitlInterruptConfig) *spec.HitlInterruptConfig {
	if c == nil {
		return nil
	}
	out := *c
	out.AllowedDecisions = append([]spec.HitlDecisionKind(nil), c.AllowedDecisions...)
	out.AllowedEditArgs = append([]string(nil), c.AllowedEditArgs...)
	out.DeniedEditArgs = append([]string(nil), c.DeniedEditArgs...)
	out.AllowedEditPaths = append([]string(nil), c.AllowedEditPaths...)
	out.DeniedEditPaths = append([]string(nil), c.DeniedEditPaths...)
	out.AllowedEditTools = append([]string(nil), c.AllowedEditTools...)
	out.RedactKeys = append([]string(nil), c.RedactKeys...)
	out.SwitchMap = cloneStringListMap(c.SwitchMap)
	return &out
}

func mergeStricterHitl(a, b *spec.HitlPolicy) *spec.HitlPolicy {
	if a == nil {
		return cloneHitlPolicy(b)
	}
	if b == nil {
		return cloneHitlPolicy(a)
	}
	out := cloneHitlPolicy(a)
	out.DescriptionPrefix = joinNonEmptyPrefix(out.DescriptionPrefix, b.DescriptionPrefix)
	out.RedactKeys = unionStrings(out.RedactKeys, b.RedactKeys)
	out.ToolSwitchMap = joinAllowMap(out.ToolSwitchMap, b.ToolSwitchMap)
	if out.InterruptOn == nil && b.InterruptOn != nil {
		out.InterruptOn = map[string]spec.HitlInterruptValue{}
	}
	for k, bv := range b.InterruptOn {
		if av, ok := out.InterruptOn[k]; ok {
			out.InterruptOn[k] = mergeHitlInterruptValue(av, bv)
		} else {
			out.InterruptOn[k] = cloneHitlInterruptValue(bv)
		}
	}
	return out
}

func mergeHitlInterruptValue(a, b spec.HitlInterruptValue) spec.HitlInterruptValue {
	out := spec.HitlInterruptValue{Enabled: a.Enabled || b.Enabled}
	if a.Config == nil && b.Config == nil {
		return out
	}
	if a.Config == nil {
		out.Config = cloneHitlInterruptConfig(b.Config)
		return out
	}
	if b.Config == nil {
		out.Config = cloneHitlInterruptConfig(a.Config)
		return out
	}
	ca, cb := a.Config, b.Config
	out.Config = &spec.HitlInterruptConfig{
		AllowedDecisions: joinDecisionAllow(ca.AllowedDecisions, cb.AllowedDecisions),
		Description:      joinNonEmptyPrefix(ca.Description, cb.Description),
		AllowedEditArgs:  joinAllowList(ca.AllowedEditArgs, cb.AllowedEditArgs),
		DeniedEditArgs:   unionStrings(ca.DeniedEditArgs, cb.DeniedEditArgs),
		AllowedEditPaths: joinAllowList(ca.AllowedEditPaths, cb.AllowedEditPaths),
		DeniedEditPaths:  unionStrings(ca.DeniedEditPaths, cb.DeniedEditPaths),
		AllowedEditTools: joinAllowList(ca.AllowedEditTools, cb.AllowedEditTools),
		SwitchMap:        joinAllowMap(ca.SwitchMap, cb.SwitchMap),
		RedactKeys:       unionStrings(ca.RedactKeys, cb.RedactKeys),
	}
	return out
}

func joinNonEmptyPrefix(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "" || a == b:
		return a
	default:
		return a + " / " + b
	}
}

func joinAllowList(a, b []string) []string {
	if len(a) == 0 {
		return append([]string(nil), b...)
	}
	if len(b) == 0 {
		return append([]string(nil), a...)
	}
	return intersectStrings(a, b)
}

func joinDecisionAllow(a, b []spec.HitlDecisionKind) []spec.HitlDecisionKind {
	if len(a) == 0 {
		return append([]spec.HitlDecisionKind(nil), b...)
	}
	if len(b) == 0 {
		return append([]spec.HitlDecisionKind(nil), a...)
	}
	seen := map[spec.HitlDecisionKind]struct{}{}
	for _, k := range b {
		seen[k] = struct{}{}
	}
	var out []spec.HitlDecisionKind
	used := map[spec.HitlDecisionKind]struct{}{}
	for _, k := range a {
		if _, ok := seen[k]; !ok {
			continue
		}
		if _, dup := used[k]; dup {
			continue
		}
		used[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

func joinAllowMap(a, b map[string][]string) map[string][]string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := map[string][]string{}
	for k, v := range a {
		out[k] = append([]string(nil), v...)
	}
	for k, v := range b {
		if prev, ok := out[k]; ok {
			out[k] = joinAllowList(prev, v)
		} else {
			out[k] = append([]string(nil), v...)
		}
	}
	return out
}

func unionStrings(a, b []string) []string {
	return uniqueStrings(append(append([]string(nil), a...), b...))
}

func intersectStrings(a, b []string) []string {
	seen := map[string]struct{}{}
	for _, s := range b {
		s = strings.TrimSpace(s)
		if s != "" {
			seen[s] = struct{}{}
		}
	}
	var out []string
	used := map[string]struct{}{}
	for _, s := range a {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; !ok {
			continue
		}
		if _, dup := used[s]; dup {
			continue
		}
		used[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func cloneStringListMap(m map[string][]string) map[string][]string {
	if m == nil {
		return nil
	}
	out := make(map[string][]string, len(m))
	for k, v := range m {
		out[k] = append([]string(nil), v...)
	}
	return out
}
