package policy

import (
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func approvalRequired(uses string, approvals *spec.PolicyApprovals) bool {
	if approvals == nil || len(approvals.RequiredFor) == 0 {
		return false
	}
	u := strings.TrimSpace(uses)
	for _, r := range approvals.RequiredFor {
		if strings.TrimSpace(r) == u {
			return true
		}
	}
	return false
}

func checkApprovalGranted(uses string, approvals *spec.PolicyApprovals, approved []string) error {
	if !approvalRequired(uses, approvals) {
		return nil
	}
	u := strings.TrimSpace(uses)
	for _, a := range approved {
		if strings.TrimSpace(a) == u {
			return nil
		}
	}
	return denied(
		ReasonApprovalRequired,
		"policy: action requires explicit approval (--approve)",
		uses,
		map[string]any{"requiredFor": uses},
	)
}
