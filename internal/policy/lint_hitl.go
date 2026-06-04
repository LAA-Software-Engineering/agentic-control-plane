package policy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools/native"
)

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

// switchTargetAllowedByPolicy reports whether a switched uses string is permitted.
// Reserved for explicit policy denylists ([DecisionDeny]); permissive policies short-circuit to true.
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
			for _, op := range editArgOperationsForTool(graph, tn) {
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

func editArgOperationsForTool(graph *spec.ProjectGraph, toolName string) []string {
	ops := workflowOperationsForTool(graph, toolName)
	if len(ops) > 0 {
		return ops
	}
	return native.DispatchOperationNames()
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
