package policy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools/native"
)

// LintSeverity classifies policy lint findings for validate/plan output (issue #107).
type LintSeverity string

const (
	LintSeverityHigh   LintSeverity = "high"
	LintSeverityMedium LintSeverity = "medium"
	LintSeverityLow    LintSeverity = "low"
)

// LintRule identifies a policy lint check (issue #107).
type LintRule string

const (
	LintRuleUngatedSensitiveTool   LintRule = "ungated_sensitive_tool"
	LintRuleInvalidSwitchTarget    LintRule = "invalid_switch_target"
	LintRuleUnknownEditArg         LintRule = "unknown_edit_arg"
	LintRuleUnreachableRequiredFor LintRule = "unreachable_required_for"
	LintRulePresetWeakened         LintRule = "preset_weakened"
	LintRuleUnknownRequiredForRef  LintRule = "unknown_required_for_ref"
)

// LintFinding is one static policy lint result.
type LintFinding struct {
	Severity LintSeverity `json:"severity"`
	Rule     LintRule     `json:"rule"`
	Message  string       `json:"message"`
	Policy   string       `json:"policy,omitempty"`
	Tool     string       `json:"tool,omitempty"`
}

// Lint runs static policy checks on a normalized, validated project graph.
func Lint(graph *spec.ProjectGraph) []LintFinding {
	if graph == nil {
		return nil
	}
	var out []LintFinding
	out = append(out, lintUngatedSensitiveTools(graph)...)
	out = append(out, lintUnknownRequiredForRefs(graph)...)
	out = append(out, lintUnreachableRequiredFor(graph)...)
	out = append(out, lintPresetWeakened(graph)...)
	out = append(out, lintHitlSwitchTargets(graph)...)
	out = append(out, lintHitlEditArgs(graph)...)
	sortLintFindings(out)
	return out
}

// HasHighSeverityLint reports whether findings contain high-severity items.
func HasHighSeverityLint(findings []LintFinding) bool {
	for _, f := range findings {
		if f.Severity == LintSeverityHigh {
			return true
		}
	}
	return false
}

// FormatLintMessage renders a finding for plan risk or validate table output.
func FormatLintMessage(f LintFinding) string {
	msg := strings.TrimSpace(f.Message)
	if msg == "" {
		return fmt.Sprintf("[%s] %s", f.Severity, f.Rule)
	}
	return fmt.Sprintf("[%s] %s", f.Severity, msg)
}

func sortLintFindings(in []LintFinding) {
	sort.Slice(in, func(i, j int) bool {
		if sevRank(in[i].Severity) != sevRank(in[j].Severity) {
			return sevRank(in[i].Severity) < sevRank(in[j].Severity)
		}
		if in[i].Policy != in[j].Policy {
			return in[i].Policy < in[j].Policy
		}
		if in[i].Tool != in[j].Tool {
			return in[i].Tool < in[j].Tool
		}
		if in[i].Rule != in[j].Rule {
			return in[i].Rule < in[j].Rule
		}
		return in[i].Message < in[j].Message
	})
}

func sevRank(s LintSeverity) int {
	switch s {
	case LintSeverityHigh:
		return 0
	case LintSeverityMedium:
		return 1
	case LintSeverityLow:
		return 2
	default:
		return 3
	}
}

func toolIsSensitive(s spec.ResolvedToolSafety) bool {
	if s.RequiresApproval {
		return true
	}
	return s.SideEffects && !s.Trusted
}

func policyExplicitlyRequiresApproval(pol *spec.PolicySpec, toolName string) bool {
	if pol == nil || pol.Approvals == nil {
		return false
	}
	if spec.ApprovalRequireAllTools(pol.Approvals) {
		return true
	}
	prefix := toolUsesPrefix(toolName)
	for _, r := range pol.Approvals.RequiredFor {
		r = strings.TrimSpace(r)
		if r == prefix || strings.HasPrefix(r, prefix) {
			return true
		}
	}
	return false
}

func toolNameFromRequiredFor(entry string) string {
	entry = strings.TrimSpace(entry)
	const prefix = "tool."
	if !strings.HasPrefix(entry, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(entry, prefix)
	i := strings.IndexByte(rest, '.')
	if i <= 0 {
		return ""
	}
	return rest[:i]
}

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

func lintHitlSwitchTargets(graph *spec.ProjectGraph) []LintFinding {
	var out []LintFinding
	for policyName, pr := range graph.Policies {
		if pr == nil || pr.Spec.Hitl == nil {
			continue
		}
		hitl := pr.Spec.Hitl
		for toolName, iv := range hitl.InterruptOn {
			tn := strings.TrimSpace(toolName)
			if tn == "" || !iv.Enabled {
				continue
			}
			tr, ok := graph.Tools[tn]
			if !ok || tr == nil {
				continue
			}
			out = append(out, lintSwitchTargetsForTool(graph, policyName, &pr.Spec, tn, tr, hitl.ToolSwitchMap, iv.Config)...)
		}
	}
	return out
}

func lintSwitchTargetsForTool(
	graph *spec.ProjectGraph,
	policyName string,
	pol *spec.PolicySpec,
	toolName string,
	tr *spec.ToolResource,
	policySwitch map[string][]string,
	cfg *spec.HitlInterruptConfig,
) []LintFinding {
	var out []LintFinding
	collect := func(sourceOp string, targets []string) {
		sourceOp = strings.TrimSpace(sourceOp)
		for _, tgt := range targets {
			tgt = strings.TrimSpace(tgt)
			if tgt == "" {
				continue
			}
			if tr.Spec.Type == "native" && !native.OperationKnown(tgt) {
				out = append(out, LintFinding{
					Severity: LintSeverityHigh,
					Rule:     LintRuleInvalidSwitchTarget,
					Policy:   policyName,
					Tool:     toolName,
					Message: fmt.Sprintf(
						"Policy/%s: hitl switch target %q is not a known native operation on Tool/%s (from %q)",
						policyName, tgt, toolName, sourceOp,
					),
				})
				continue
			}
			uses := fmt.Sprintf("tool.%s.%s", toolName, tgt)
			if _, _, err := tools.ParseUses(uses); err != nil {
				out = append(out, LintFinding{
					Severity: LintSeverityHigh,
					Rule:     LintRuleInvalidSwitchTarget,
					Policy:   policyName,
					Tool:     toolName,
					Message: fmt.Sprintf(
						"Policy/%s: hitl switch target %q is not a valid uses string on Tool/%s",
						policyName, tgt, toolName,
					),
				})
				continue
			}
			if !switchTargetAllowedByPolicy(graph, pol, uses) {
				out = append(out, LintFinding{
					Severity: LintSeverityHigh,
					Rule:     LintRuleInvalidSwitchTarget,
					Policy:   policyName,
					Tool:     toolName,
					Message: fmt.Sprintf(
						"Policy/%s: hitl switch target tool.%s.%s is not allowed under policy rules",
						policyName, toolName, tgt,
					),
				})
			}
		}
	}
	if policySwitch != nil {
		for src, targets := range policySwitch {
			collect(src, targets)
		}
	}
	if cfg != nil && cfg.SwitchMap != nil {
		for src, targets := range cfg.SwitchMap {
			collect(src, targets)
		}
	}
	if cfg != nil {
		for _, tgtTool := range cfg.AllowedEditTools {
			tgtTool = strings.TrimSpace(tgtTool)
			if tgtTool == "" {
				continue
			}
			if _, ok := graph.Tools[tgtTool]; !ok {
				out = append(out, LintFinding{
					Severity: LintSeverityHigh,
					Rule:     LintRuleInvalidSwitchTarget,
					Policy:   policyName,
					Tool:     toolName,
					Message: fmt.Sprintf(
						"Policy/%s: hitl allowedEditTools %q references missing Tool/%s",
						policyName, tgtTool, tgtTool,
					),
				})
			}
		}
	}
	return out
}

func switchTargetAllowedByPolicy(graph *spec.ProjectGraph, pol *spec.PolicySpec, uses string) bool {
	toolName, _, err := tools.ParseUses(uses)
	if err != nil {
		return false
	}
	if pol != nil && pol.Approvals != nil && spec.ApprovalPermissive(pol.Approvals) {
		return true
	}
	td := EffectiveToolDecision(graph, pol, toolName)
	return td.Decision != DecisionDeny
}

func lintHitlEditArgs(graph *spec.ProjectGraph) []LintFinding {
	var out []LintFinding
	for policyName, pr := range graph.Policies {
		if pr == nil || pr.Spec.Hitl == nil {
			continue
		}
		for toolName, iv := range pr.Spec.Hitl.InterruptOn {
			tn := strings.TrimSpace(toolName)
			if tn == "" || !iv.Enabled || iv.Config == nil {
				continue
			}
			tr, ok := graph.Tools[tn]
			if !ok || tr == nil || tr.Spec.Type != "native" {
				continue
			}
			ops := workflowOperationsForTool(graph, tn)
			if len(ops) == 0 {
				ops = []string{"echo"}
			}
			for _, op := range ops {
				known, haveSchema := native.TopLevelArgsForOperation(op)
				if !haveSchema {
					continue
				}
				if known == nil {
					continue
				}
				knownSet := make(map[string]struct{}, len(known))
				for _, k := range known {
					knownSet[k] = struct{}{}
				}
				out = append(out, lintEditArgRefs(policyName, tn, op, iv.Config.AllowedEditArgs, knownSet, "allowedEditArgs")...)
				out = append(out, lintEditArgRefs(policyName, tn, op, iv.Config.DeniedEditArgs, knownSet, "deniedEditArgs")...)
			}
		}
	}
	return out
}

func lintEditArgRefs(policyName, toolName, operation string, args []string, known map[string]struct{}, field string) []LintFinding {
	var out []LintFinding
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" || arg == "*" {
			continue
		}
		if _, ok := known[arg]; ok {
			continue
		}
		out = append(out, LintFinding{
			Severity: LintSeverityMedium,
			Rule:     LintRuleUnknownEditArg,
			Policy:   policyName,
			Tool:     toolName,
			Message: fmt.Sprintf(
				"Policy/%s: hitl %s %q is not in Tool/%s operation %q input schema",
				policyName, field, arg, toolName, operation,
			),
		})
	}
	return out
}

func workflowOperationsForTool(graph *spec.ProjectGraph, toolName string) []string {
	seen := make(map[string]struct{})
	var out []string
	prefix := "tool." + toolName + "."
	for _, wr := range graph.Workflows {
		if wr == nil {
			continue
		}
		for _, st := range wr.Spec.Steps {
			uses := strings.TrimSpace(st.Uses)
			if !strings.HasPrefix(uses, prefix) {
				continue
			}
			op := strings.TrimPrefix(uses, prefix)
			if op == "" {
				continue
			}
			if _, ok := seen[op]; ok {
				continue
			}
			seen[op] = struct{}{}
			out = append(out, op)
		}
	}
	sort.Strings(out)
	return out
}
