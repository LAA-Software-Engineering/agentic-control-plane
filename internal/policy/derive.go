package policy

import (
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
)

// Decision is the effective policy outcome for a tool call when explicit Policy rules do not apply.
type Decision string

const (
	// DecisionAllow permits unattended execution.
	DecisionAllow Decision = "allow"
	// DecisionRequireApproval blocks until the full uses string is passed via --approve.
	DecisionRequireApproval Decision = "requireApproval"
	// DecisionDeny hard-blocks execution (explicit denylist only; metadata fallback uses requireApproval).
	DecisionDeny Decision = "deny"
)

// DecisionSource explains why a [Decision] was chosen (plan risk surfacing, issue #103).
type DecisionSource string

const (
	SourceExplicitPolicyRule DecisionSource = "explicit_policy_rule"
	SourceSafetyMetadata     DecisionSource = "safety_metadata"
	SourceFailClosedDefault  DecisionSource = "fail_closed_default"
)

// ToolDecision bundles an effective decision and its provenance for a tool resource.
type ToolDecision struct {
	Decision Decision
	Source   DecisionSource
	Safety   spec.ResolvedToolSafety
}

// Derive maps resolved safety metadata to a fallback decision (issue #103 truth table).
// Metadata fallback never returns [DecisionDeny]; use explicit policy denylists for hard denies.
func Derive(safety spec.ResolvedToolSafety) Decision {
	if safety.RequiresApproval {
		return DecisionRequireApproval
	}
	return DecisionAllow
}

// EffectiveToolDecision returns the decision for toolName under policy pol (explicit rules first).
func EffectiveToolDecision(graph *spec.ProjectGraph, pol *spec.PolicySpec, toolName string) ToolDecision {
	toolName = strings.TrimSpace(toolName)
	safety := resolvedSafetyForTool(graph, toolName)
	if pol != nil && pol.Approvals != nil {
		if pol.Approvals.Permissive {
			return ToolDecision{
				Decision: DecisionAllow,
				Source:   SourceExplicitPolicyRule,
				Safety:   safety,
			}
		}
		if pol.Approvals.RequireAllTools {
			return ToolDecision{
				Decision: DecisionRequireApproval,
				Source:   SourceExplicitPolicyRule,
				Safety:   safety,
			}
		}
		if spec.ResolvedPresetName(pol) == spec.PresetShellSafe {
			if safety.RequiresApproval || safety.SideEffects {
				return ToolDecision{
					Decision: DecisionRequireApproval,
					Source:   SourceExplicitPolicyRule,
					Safety:   safety,
				}
			}
		}
	}
	if pol != nil && pol.Approvals != nil {
		prefix := toolUsesPrefix(toolName)
		for _, r := range pol.Approvals.RequiredFor {
			r = strings.TrimSpace(r)
			if r == prefix || strings.HasPrefix(r, prefix) {
				return ToolDecision{
					Decision: DecisionRequireApproval,
					Source:   SourceExplicitPolicyRule,
					Safety:   safety,
				}
			}
		}
	}
	src := SourceSafetyMetadata
	if graph == nil || graph.Tools == nil {
		src = SourceFailClosedDefault
	} else if tr, ok := graph.Tools[toolName]; !ok || tr == nil || tr.Spec.Safety == nil {
		src = SourceFailClosedDefault
	}
	return ToolDecision{
		Decision: Derive(safety),
		Source:   src,
		Safety:   safety,
	}
}

func resolvedSafetyForTool(graph *spec.ProjectGraph, toolName string) spec.ResolvedToolSafety {
	if graph == nil || graph.Tools == nil {
		return spec.ResolveToolSafety(nil)
	}
	tr, ok := graph.Tools[toolName]
	if !ok || tr == nil {
		return spec.ResolveToolSafety(nil)
	}
	return spec.ResolveToolSafety(tr.Spec.Safety)
}

func checkSafetyDerived(graph *spec.ProjectGraph, call ToolCallContext) error {
	toolName, _, err := tools.ParseUses(call.Uses)
	if err != nil {
		return denied(ReasonInvalidUses, fmt.Sprintf("policy: %v", err), call.Uses, nil)
	}
	td := EffectiveToolDecision(graph, nil, toolName)
	switch td.Decision {
	case DecisionAllow:
		return nil
	case DecisionRequireApproval:
		if actionApproved(call.Uses, call.Run.ApprovedActions) {
			return nil
		}
		return denied(
			ReasonApprovalRequired,
			"policy: tool requires approval from safety metadata (--approve)",
			call.Uses,
			map[string]any{
				"tool":   toolName,
				"source": string(td.Source),
			},
		)
	default:
		// Derive never returns DecisionDeny; reserved for future explicit denylists.
		return denied(
			ReasonDenied,
			fmt.Sprintf("policy: unexpected tool decision %q", td.Decision),
			call.Uses,
			map[string]any{"tool": toolName},
		)
	}
}

// toolUsesPrefix is the plan-risk prefix for tool.<name>. (conservative; runtime uses exact uses).
func toolUsesPrefix(toolName string) string {
	return "tool." + strings.TrimSpace(toolName) + "."
}

func actionApproved(uses string, approved []string) bool {
	u := strings.TrimSpace(uses)
	for _, a := range approved {
		if strings.TrimSpace(a) == u {
			return true
		}
	}
	return false
}
