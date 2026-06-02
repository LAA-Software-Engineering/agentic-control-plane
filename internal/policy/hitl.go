package policy

import (
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
)

// ResolvedHitlReview is the merged review configuration for one gated tool call.
type ResolvedHitlReview struct {
	Description      string
	AllowedDecisions []spec.HitlDecisionKind
	AllowedEditArgs  []string
	DeniedEditArgs   []string
	AllowedEditPaths []string
	DeniedEditPaths  []string
	SwitchTargets    []string
	RedactKeys       []string
}

// HitlGate describes a tool call that requires human approval before execution.
type HitlGate struct {
	Uses   string
	With   map[string]any
	Review ResolvedHitlReview
}

// ResolveHitlReview merges policy-level and per-tool HITL config for a tool call.
func ResolveHitlReview(graph *spec.ProjectGraph, pol *spec.PolicySpec, uses string) (ResolvedHitlReview, error) {
	toolName, operation, err := tools.ParseUses(uses)
	if err != nil {
		return ResolvedHitlReview{}, err
	}
	var cfg *spec.HitlInterruptConfig
	if pol != nil && pol.Hitl != nil {
		if iv, ok := pol.Hitl.InterruptOn[toolName]; ok && iv.Enabled {
			cfg = iv.Config
		}
	}
	prefix := spec.DefaultHitlDescriptionPrefix
	var redact []string
	var switchMap map[string][]string
	if pol != nil && pol.Hitl != nil {
		if p := strings.TrimSpace(pol.Hitl.DescriptionPrefix); p != "" {
			prefix = p
		}
		redact = append(redact, pol.Hitl.RedactKeys...)
		switchMap = pol.Hitl.ToolSwitchMap
	}
	desc := prefix
	if cfg != nil && strings.TrimSpace(cfg.Description) != "" {
		desc = renderHitlDescription(cfg.Description, uses, toolName, operation)
	} else {
		desc = fmt.Sprintf("%s: %s", prefix, uses)
	}
	review := ResolvedHitlReview{
		Description:      desc,
		AllowedDecisions: defaultHitlDecisions(cfg, switchMap, operation),
		RedactKeys:       uniqueStrings(append(redact, hitlConfigRedact(cfg)...)),
	}
	if cfg != nil {
		if len(cfg.AllowedDecisions) > 0 {
			review.AllowedDecisions = append([]spec.HitlDecisionKind(nil), cfg.AllowedDecisions...)
		}
		review.AllowedEditArgs = append([]string(nil), cfg.AllowedEditArgs...)
		review.DeniedEditArgs = append([]string(nil), cfg.DeniedEditArgs...)
		review.AllowedEditPaths = append([]string(nil), cfg.AllowedEditPaths...)
		review.DeniedEditPaths = append([]string(nil), cfg.DeniedEditPaths...)
		review.RedactKeys = uniqueStrings(append(review.RedactKeys, cfg.RedactKeys...))
	}
	review.SwitchTargets = resolveSwitchTargets(cfg, switchMap, operation)
	if cfg != nil && len(cfg.AllowedEditTools) > 0 {
		review.SwitchTargets = uniqueStrings(append(review.SwitchTargets, cfg.AllowedEditTools...))
	}
	return review, nil
}

func hitlConfigRedact(cfg *spec.HitlInterruptConfig) []string {
	if cfg == nil {
		return nil
	}
	return cfg.RedactKeys
}

func defaultHitlDecisions(cfg *spec.HitlInterruptConfig, policySwitch map[string][]string, operation string) []spec.HitlDecisionKind {
	if cfg != nil && len(cfg.AllowedDecisions) > 0 {
		return append([]spec.HitlDecisionKind(nil), cfg.AllowedDecisions...)
	}
	out := []spec.HitlDecisionKind{spec.HitlDecisionApprove, spec.HitlDecisionReject}
	if cfg != nil && (len(cfg.AllowedEditArgs) > 0 || len(cfg.AllowedEditPaths) > 0 || len(cfg.DeniedEditArgs) > 0 || len(cfg.DeniedEditPaths) > 0) {
		out = append(out, spec.HitlDecisionEdit)
	}
	if hasSwitchTargets(cfg, policySwitch, operation) {
		out = append(out, spec.HitlDecisionSwitch)
	}
	return out
}

func hasSwitchTargets(cfg *spec.HitlInterruptConfig, policySwitch map[string][]string, operation string) bool {
	if cfg != nil {
		if len(cfg.AllowedEditTools) > 0 {
			return true
		}
		if t, ok := cfg.SwitchMap[operation]; ok && len(t) > 0 {
			return true
		}
	}
	if t, ok := policySwitch[operation]; ok && len(t) > 0 {
		return true
	}
	return false
}

func resolveSwitchTargets(cfg *spec.HitlInterruptConfig, policySwitch map[string][]string, operation string) []string {
	var out []string
	if cfg != nil {
		out = append(out, cfg.AllowedEditTools...)
		if t, ok := cfg.SwitchMap[operation]; ok {
			out = append(out, t...)
		}
	}
	if t, ok := policySwitch[operation]; ok {
		out = append(out, t...)
	}
	return uniqueStrings(out)
}

func renderHitlDescription(tmpl, uses, toolName, operation string) string {
	r := strings.NewReplacer(
		"${tool}", toolName,
		"${uses}", uses,
		"${operation}", operation,
	)
	return r.Replace(tmpl)
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// ToolCallNeedsHitl reports whether call requires human approval and is not pre-approved.
func ToolCallNeedsHitl(graph *spec.ProjectGraph, pol *spec.PolicySpec, call ToolCallContext) (bool, error) {
	if actionApproved(call.Uses, call.Run.ApprovedActions) {
		return false, nil
	}
	ev := NewEvaluator(graph, pol)
	if err := ev.CheckToolCall(nil, call); err == nil {
		return false, nil
	} else if d, ok := AsDenied(err); ok && d.Reason == ReasonApprovalRequired {
		return true, nil
	} else {
		return false, err
	}
}

// BuildHitlGate constructs a [HitlGate] when the call is approval-gated and not pre-approved.
func BuildHitlGate(graph *spec.ProjectGraph, pol *spec.PolicySpec, call ToolCallContext) (*HitlGate, error) {
	need, err := ToolCallNeedsHitl(graph, pol, call)
	if err != nil {
		return nil, err
	}
	if !need {
		return nil, nil
	}
	review, err := ResolveHitlReview(graph, pol, call.Uses)
	if err != nil {
		return nil, err
	}
	with := call.With
	if with == nil {
		with = map[string]any{}
	}
	return &HitlGate{Uses: call.Uses, With: with, Review: review}, nil
}
