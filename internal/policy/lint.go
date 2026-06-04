package policy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
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
//
// Ungated-sensitive-tool findings require explicit Policy approvals (requiredFor or
// requireAllTools), not merely fail-closed safety metadata that would gate at run time.
// Use validate --strict to fail on high-severity findings; default validate is advisory.
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
