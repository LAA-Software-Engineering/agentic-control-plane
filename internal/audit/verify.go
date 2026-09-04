package audit

import (
	"errors"
	"fmt"

	"github.com/Terfyn/terfyn/internal/state"
)

// ErrChainBroken indicates a hash or prev_hash mismatch in a run's trace chain.
var ErrChainBroken = errors.New("audit: chain broken")

// Broken-field values reported by [VerifyRunResult.BrokenField] and audit verify JSON output.
const (
	BrokenFieldPartialChain       = "partial_chain"
	BrokenFieldPrevHash           = "prev_hash"
	BrokenFieldHash               = "hash"
	BrokenFieldHashCompute        = "hash_compute"
	BrokenFieldUnchainedInsertion = "unchained_insertion"
)

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
			// An unchained row is tolerated ONLY as a leading prefix: rows written
			// before migration 007 introduced chaining can only be a prefix of a run's
			// events (e.g. a run interrupted pre-migration and resumed after it). Every
			// row appended since then sets both hash and prev_hash (AppendTraceEvent),
			// so an unchained row that appears AFTER a chained row can only be a forged
			// insertion — reject it instead of silently skipping it regardless of
			// position, which let an attacker append/insert an unhashed row past a green
			// verify (#383, #396).
			if res.Chained > 0 {
				res.BrokenSeq = e.Seq
				res.BrokenField = BrokenFieldUnchainedInsertion
				return res
			}
			res.Unchained++
			continue
		}
		if e.Hash == "" || e.PrevHash == "" {
			res.BrokenSeq = e.Seq
			res.BrokenField = BrokenFieldPartialChain
			return res
		}

		res.Chained++
		expectedPrev := GenesisHash(runID)
		if lastChainedHash != "" {
			expectedPrev = lastChainedHash
		}
		if e.PrevHash != expectedPrev {
			res.BrokenSeq = e.Seq
			res.BrokenField = BrokenFieldPrevHash
			return res
		}

		got, err := EventHash(e, e.PrevHash)
		if err != nil {
			res.BrokenSeq = e.Seq
			res.BrokenField = BrokenFieldHashCompute
			return res
		}
		if got != e.Hash {
			res.BrokenSeq = e.Seq
			res.BrokenField = BrokenFieldHash
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
