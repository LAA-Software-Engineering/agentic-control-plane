package plan

import (
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// WorkflowSpecHash returns the deployment spec_hash for a normalized workflow resource envelope.
func WorkflowSpecHash(wf *spec.WorkflowResource) (string, error) {
	if wf == nil {
		return "", fmt.Errorf("plan: nil workflow")
	}
	raw, err := canonicalResourceJSON(wf)
	if err != nil {
		return "", fmt.Errorf("plan: canonical json for workflow: %w", err)
	}
	return SpecHashHex(raw), nil
}
