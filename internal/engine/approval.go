package engine

import (
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/terfyn/internal/policy"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

// approvalHitlGate builds the HITL gate for a workflow-level approval node. The
// execir InvokeApproval path uses it to present the reviewed payload and to
// resolve the operator decision through the shared HITL machinery.
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
