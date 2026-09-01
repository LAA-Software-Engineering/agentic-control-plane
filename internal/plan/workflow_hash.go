package plan

import (
	"fmt"

	"github.com/Terfyn/terfyn/internal/spec"
)

// WorkflowSpecHash returns the deployment spec_hash for a normalized workflow
// resource envelope, hashing the RESOURCE projection only. Use it only where a
// workflow provably has no execution IR; otherwise a run executes a pinned
// Program the hash does not commit to. See [WorkflowSpecHashWithExec].
func WorkflowSpecHash(wf *spec.WorkflowResource) (string, error) {
	return WorkflowSpecHashWithExec(wf, "")
}

// WorkflowSpecHashWithExec returns the deployment spec_hash for a workflow: a
// workflow's identity is its normalized resource projection AND the exact
// executable IR (execir.Program) that runs it, whenever one exists (issue #260,
// ADR 002 §5).
//
// The invariant is deliberately not tied to any node kind. After #256 both
// ingress paths lower to an execir.Program, and #260 pins that program into the
// deployment snapshot and executes it — so the program is execution authority.
// Two workflows with an identical resource projection but a different Program
// (a lowering/compiler change, or an `.agent` control-flow restructuring the
// flattened resource projection cannot express) must therefore get a different
// spec_hash, or a stale plan/snapshot would apply and a run would execute
// something the identity did not commit to. Folding execir.Program.Digest closes
// that gap by construction, without asking "does this Program contain
// information the resource projection couldn't represent?".
//
// execDigest is empty only for a workflow with no lowerable program, in which
// case the hash is exactly the historical resource-only hash.
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
