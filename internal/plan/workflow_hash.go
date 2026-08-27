package plan

import (
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// WorkflowSpecHash returns the deployment spec_hash for a normalized workflow resource envelope.
func WorkflowSpecHash(wf *spec.WorkflowResource) (string, error) {
	return WorkflowSpecHashWithExec(wf, "")
}

// WorkflowSpecHashWithExec returns the deployment spec_hash for a workflow,
// folding in the digest of its execution IR (ADR 002 §5, issue #199). The
// resource projection alone cannot represent control flow (Branch/Loop), so two
// workflows with identical resource projections but different lowered programs —
// e.g. an `if` whose two arms invoke the same set of operations in a different
// structure — would otherwise share a spec_hash and let a stale plan apply. The
// execution-IR digest (execir.Program.Digest) closes that gap: a lowering change
// with no resource-level change still changes the hash.
//
// execDigest is empty for a workflow with no execution IR (a straight-line YAML
// workflow), in which case the hash is exactly the historical resource-only
// hash — existing deployment state and golden hashes are unaffected.
func WorkflowSpecHashWithExec(wf *spec.WorkflowResource, execDigest string) (string, error) {
	if wf == nil {
		return "", fmt.Errorf("plan: nil workflow")
	}
	raw, err := canonicalResourceJSON(wf)
	if err != nil {
		return "", fmt.Errorf("plan: canonical json for workflow: %w", err)
	}
	if execDigest == "" {
		return SpecHashHex(raw), nil
	}
	// Fold the execution-IR digest into the hashed bytes. A newline separator
	// keeps the two components unambiguous.
	folded := append(raw, '\n')
	folded = append(folded, "execir:"...)
	folded = append(folded, execDigest...)
	return SpecHashHex(folded), nil
}
