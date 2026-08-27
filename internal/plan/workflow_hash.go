package plan

import (
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// WorkflowSpecHash returns the deployment spec_hash for a normalized workflow
// resource envelope.
//
// It hashes the RESOURCE projection only. A workflow lowered from `.agent` with
// control flow must instead go through [WorkflowSpecHashWithExec] so its
// execution-IR digest folds in — this bare form would give a control-flow-only
// edit the historical hash. Every current caller passes a YAML-loaded workflow
// with no execution IR, so this is correct today; when `.agent` ingest is wired
// into plan/run (a follow-up), those call sites must switch to the WithExec
// form. See ADR 002 §5.
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
// This is the FOLD MECHANISM, not yet a wired invariant: no production path
// constructs an execir.Program for a workflow yet (`.agent` is not ingested by
// project.LoadProject, and plan hashes YAML resource envelopes with no execir
// parameter), so nothing calls this with a non-empty digest outside tests. It
// exists so the `.agent` ingest follow-up folds the digest here rather than
// reinventing it. execDigest is empty for a workflow with no execution IR, in
// which case the hash is exactly the historical resource-only hash — existing
// deployment state and golden hashes are unaffected.
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
