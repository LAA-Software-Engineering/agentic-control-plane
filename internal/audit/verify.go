package audit

import (
	"errors"
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// ErrChainBroken indicates a hash or prev_hash mismatch in a run's trace chain.
var ErrChainBroken = errors.New("audit: chain broken")

// VerifyRunResult is the outcome of verifying one run's trace hash chain.
type VerifyRunResult struct {
	RunID       string
	Total       int
	Chained     int
	Unchained   int
	BrokenSeq   int64
	BrokenField string
}

// Ok reports whether the chain has no breaks. Unchained pre-migration rows do not fail verification.
func (r VerifyRunResult) Ok() bool {
	return r.BrokenSeq == 0
}

// VerifyRunChain re-derives hashes for chained events in events (ordered by seq ascending).
func VerifyRunChain(runID string, events []state.TraceEvent) VerifyRunResult {
	res := VerifyRunResult{RunID: runID, Total: len(events)}
	var lastChainedHash string

	for _, e := range events {
		if e.Hash == "" && e.PrevHash == "" {
			res.Unchained++
			continue
		}
		if e.Hash == "" || e.PrevHash == "" {
			res.BrokenSeq = e.Seq
			res.BrokenField = "partial_chain"
			return res
		}

		res.Chained++
		expectedPrev := GenesisHash(runID)
		if lastChainedHash != "" {
			expectedPrev = lastChainedHash
		}
		if e.PrevHash != expectedPrev {
			res.BrokenSeq = e.Seq
			res.BrokenField = "prev_hash"
			return res
		}

		got, err := EventHash(e, e.PrevHash)
		if err != nil {
			res.BrokenSeq = e.Seq
			res.BrokenField = "hash_compute"
			return res
		}
		if got != e.Hash {
			res.BrokenSeq = e.Seq
			res.BrokenField = "hash"
			return res
		}
		lastChainedHash = e.Hash
	}
	return res
}

// VerifyRunChainError wraps [VerifyRunResult] as an error when the chain is broken.
func VerifyRunChainError(runID string, events []state.TraceEvent) error {
	res := VerifyRunChain(runID, events)
	if res.Ok() {
		return nil
	}
	return fmt.Errorf("%w at run %q seq %d (%s)", ErrChainBroken, runID, res.BrokenSeq, res.BrokenField)
}
