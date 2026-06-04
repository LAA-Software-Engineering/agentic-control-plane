package policy

import (
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func lintUngatedSensitiveTools(graph *spec.ProjectGraph) []LintFinding {
	var out []LintFinding
	for policyName, pr := range graph.Policies {
		if pr == nil {
			continue
		}
		pol := &pr.Spec
		permissive := pol.Approvals != nil && spec.ApprovalPermissive(pol.Approvals)
		for toolName, tr := range graph.Tools {
			if tr == nil {
				continue
			}
			safety := spec.ResolveToolSafety(tr.Spec.Safety)
			if !toolIsSensitive(safety) {
				continue
			}
			td := EffectiveToolDecision(graph, pol, toolName)
			if permissive && td.Decision == DecisionAllow {
				out = append(out, LintFinding{
					Severity: LintSeverityHigh,
					Rule:     LintRuleUngatedSensitiveTool,
					Policy:   policyName,
					Tool:     toolName,
					Message: fmt.Sprintf(
						"Policy/%s: permissive approvals allow unattended Tool/%s (sensitive side effects or requiresApproval)",
						policyName, toolName,
					),
				})
				continue
			}
			if !policyExplicitlyRequiresApproval(pol, toolName) {
				out = append(out, LintFinding{
					Severity: LintSeverityHigh,
					Rule:     LintRuleUngatedSensitiveTool,
					Policy:   policyName,
					Tool:     toolName,
					Message: fmt.Sprintf(
						"Policy/%s: sensitive Tool/%s has no explicit approval rule (requiredFor or requireAllTools)",
						policyName, toolName,
					),
				})
			}
		}
	}
	return out
}

func lintUnknownRequiredForRefs(graph *spec.ProjectGraph) []LintFinding {
	var out []LintFinding
	for policyName, pr := range graph.Policies {
		if pr == nil || pr.Spec.Approvals == nil {
			continue
		}
		for _, entry := range pr.Spec.Approvals.RequiredFor {
			tn := toolNameFromRequiredFor(entry)
			if tn == "" {
				continue
			}
			if _, ok := graph.Tools[tn]; ok {
				continue
			}
			out = append(out, LintFinding{
				Severity: LintSeverityHigh,
				Rule:     LintRuleUnknownRequiredForRef,
				Policy:   policyName,
				Tool:     tn,
				Message: fmt.Sprintf(
					"Policy/%s: approvals.requiredFor %q references missing Tool/%s",
					policyName, strings.TrimSpace(entry), tn,
				),
			})
		}
	}
	return out
}

func lintUnreachableRequiredFor(graph *spec.ProjectGraph) []LintFinding {
	var out []LintFinding
	for policyName, pr := range graph.Policies {
		if pr == nil || pr.Spec.Approvals == nil {
			continue
		}
		if !spec.ApprovalRequireAllTools(pr.Spec.Approvals) {
			continue
		}
		for _, entry := range pr.Spec.Approvals.RequiredFor {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			out = append(out, LintFinding{
				Severity: LintSeverityLow,
				Rule:     LintRuleUnreachableRequiredFor,
				Policy:   policyName,
				Message: fmt.Sprintf(
					"Policy/%s: approvals.requiredFor %q is redundant when requireAllTools is true",
					policyName, entry,
				),
			})
		}
	}
	return out
}

func lintPresetWeakened(graph *spec.ProjectGraph) []LintFinding {
	var out []LintFinding
	for policyName, pr := range graph.Policies {
		if pr == nil {
			continue
		}
		basePreset := strings.TrimSpace(pr.Spec.Preset)
		if basePreset == "" {
			continue
		}
		if !spec.IsBuiltinPreset(basePreset) {
			continue
		}
		base, err := spec.BuildPreset(basePreset)
		if err != nil {
			continue
		}
		resolved := pr.Spec
		if resolved.ResolvedPreset == "" {
			if merged, err := spec.ResolvePolicySpec(&pr.Spec); err == nil && merged != nil {
				resolved = *merged
			}
		}
		if basePreset == spec.PresetStrict {
			if resolved.Approvals != nil && spec.ApprovalPermissive(resolved.Approvals) {
				out = append(out, LintFinding{
					Severity: LintSeverityHigh,
					Rule:     LintRulePresetWeakened,
					Policy:   policyName,
					Message: fmt.Sprintf(
						"Policy/%s: preset %q overridden with approvals.permissive (less safe than strict)",
						policyName, basePreset,
					),
				})
			}
			if base.Approvals != nil && spec.ApprovalRequireAllTools(base.Approvals) {
				if resolved.Approvals != nil && resolved.Approvals.RequireAllTools != nil && !*resolved.Approvals.RequireAllTools {
					out = append(out, LintFinding{
						Severity: LintSeverityHigh,
						Rule:     LintRulePresetWeakened,
						Policy:   policyName,
						Message: fmt.Sprintf(
							"Policy/%s: preset %q overridden with approvals.requireAllTools: false",
							policyName, basePreset,
						),
					})
				}
			}
		}
	}
	return out
}
