package audit

import (
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/state"
)

func TestGenesisHash_stablePerRun(t *testing.T) {
	a := GenesisHash("run-a")
	b := GenesisHash("run-a")
	c := GenesisHash("run-b")
	if a != b {
		t.Fatal("genesis not stable")
	}
	if a == c {
		t.Fatal("genesis must differ by run id")
	}
	if len(a) != 64 {
		t.Fatalf("genesis len=%d want 64 hex chars", len(a))
	}
}

func TestCanonicalEventBytes_deterministic(t *testing.T) {
	ts := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	e := state.TraceEvent{
		RunID: "r1", Seq: 1, Timestamp: ts,
		Type: "run_started", ActorType: "agent",
		DataJSON: `{"k":1}`,
		TenantID: "t1", ThreadID: "th1", ActorID: "a1",
	}
	b1, err := CanonicalEventBytes(e)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := CanonicalEventBytes(e)
	if err != nil {
		t.Fatal(err)
	}
	if string(b1) != string(b2) {
		t.Fatalf("non-deterministic: %q vs %q", b1, b2)
	}
}

func TestEventHash_changesWithPrevHash(t *testing.T) {
	ts := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	e := state.TraceEvent{
		RunID: "r1", Seq: 1, Timestamp: ts,
		Type: "run_started", ActorType: "agent", DataJSON: `{}`,
	}
	h1, err := EventHash(e, GenesisHash("r1"))
	if err != nil {
		t.Fatal(err)
	}
	h2, err := EventHash(e, GenesisHash("other"))
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Fatal("hash must depend on prev_hash")
	}
}

func TestPrevHashForAppend_skipsUnchainedTail(t *testing.T) {
	runID := "r1"
	gen := GenesisHash(runID)
	prior := []state.TraceEvent{
		{Seq: 1, Hash: "abc"},
		{Seq: 2, Hash: ""},
	}
	if got := PrevHashForAppend(runID, prior); got != "abc" {
		t.Fatalf("prev=%q want abc", got)
	}
	if got := PrevHashForAppend(runID, nil); got != gen {
		t.Fatalf("prev=%q want genesis", got)
	}
	if got := PrevHashForAppend(runID, []state.TraceEvent{{Seq: 1}}); got != gen {
		t.Fatalf("prev=%q want genesis for unchained only", got)
	}
}

func TestVerifyRunChain_okSingleEvent(t *testing.T) {
	runID := "r1"
	ts := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	e := state.TraceEvent{
		RunID: runID, Seq: 1, Timestamp: ts,
		Type: "run_started", ActorType: "agent", DataJSON: `{}`,
	}
	prev := GenesisHash(runID)
	hash, err := EventHash(e, prev)
	if err != nil {
		t.Fatal(err)
	}
	e.PrevHash = prev
	e.Hash = hash

	res := VerifyRunChain(runID, []state.TraceEvent{e})
	if !res.Ok() || res.Chained != 1 || res.Unchained != 0 {
		t.Fatalf("res=%+v", res)
	}
}

func TestVerifyRunChain_okMultiEvent(t *testing.T) {
	runID := "r1"
	ts := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	var events []state.TraceEvent
	prev := GenesisHash(runID)
	for seq := int64(1); seq <= 3; seq++ {
		e := state.TraceEvent{
			RunID: runID, Seq: seq, Timestamp: ts.Add(time.Duration(seq) * time.Second),
			Type: "tool_execution", ActorType: "agent",
			StepID: "s1", DataJSON: `{"n":` + string(rune('0'+seq)) + `}`,
		}
		hash, err := EventHash(e, prev)
		if err != nil {
			t.Fatal(err)
		}
		e.PrevHash = prev
		e.Hash = hash
		events = append(events, e)
		prev = hash
	}
	res := VerifyRunChain(runID, events)
	if !res.Ok() || res.Chained != 3 {
		t.Fatalf("res=%+v", res)
	}
}

func TestVerifyRunChain_unchainedIgnored(t *testing.T) {
	runID := "legacy"
	ts := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	unchained := state.TraceEvent{
		RunID: runID, Seq: 1, Timestamp: ts,
		Type: "run_started", ActorType: "agent", DataJSON: `{}`,
	}
	chained := state.TraceEvent{
		RunID: runID, Seq: 2, Timestamp: ts.Add(time.Second),
		Type: "run_finished", ActorType: "agent", DataJSON: `{}`,
	}
	prev := GenesisHash(runID)
	hash, err := EventHash(chained, prev)
	if err != nil {
		t.Fatal(err)
	}
	chained.PrevHash = prev
	chained.Hash = hash

	res := VerifyRunChain(runID, []state.TraceEvent{unchained, chained})
	if !res.Ok() || res.Unchained != 1 || res.Chained != 1 {
		t.Fatalf("res=%+v", res)
	}
}

// TestVerifyRunChain_rejectsAppendedUnchainedEvent proves a forged unchained row
// appended after the chain's tip fails verification (#383) — the reported attack:
// an inserted row with NULL hash/prev_hash used to pass as a tolerated "unchained"
// row regardless of position.
func TestVerifyRunChain_rejectsAppendedUnchainedEvent(t *testing.T) {
	runID := "r1"
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(seq int64, typ, prev string) state.TraceEvent {
		e := state.TraceEvent{RunID: runID, Seq: seq, Timestamp: ts, Type: typ, ActorType: "agent", DataJSON: "{}", PrevHash: prev}
		h, err := EventHash(e, prev)
		if err != nil {
			t.Fatal(err)
		}
		e.Hash = h
		return e
	}
	e1 := mk(1, "run_started", GenesisHash(runID))
	e2 := mk(2, "run_finished", e1.Hash)
	forged := state.TraceEvent{RunID: runID, Seq: 3, Timestamp: ts, Type: "tool_execution", ActorType: "agent", DataJSON: `{"uses":"tool.x.y","success":true}`}
	res := VerifyRunChain(runID, []state.TraceEvent{e1, e2, forged})
	if res.Ok() {
		t.Fatalf("forged unchained event appended after chained rows passed verification: %+v", res)
	}
	if res.BrokenSeq != 3 || res.BrokenField != BrokenFieldUnchainedInsertion {
		t.Fatalf("res=%+v", res)
	}
}

func TestPrevHashForChainTip(t *testing.T) {
	gen := GenesisHash("r1")
	if got := PrevHashForChainTip("r1", ""); got != gen {
		t.Fatalf("got %q want genesis", got)
	}
	if got := PrevHashForChainTip("r1", "abc"); got != "abc" {
		t.Fatalf("got %q want abc", got)
	}
}

// TestVerifyRunChain_rejectsMiddleUnchainedInsertion proves an unchained row
// sandwiched between chained rows (the chain links past it) is rejected as a forged
// insertion, not silently skipped — every post-migration row is chained, so an
// unchained row after a chained one can only be tampering (#383, #396).
func TestVerifyRunChain_rejectsMiddleUnchainedInsertion(t *testing.T) {
	runID := "r1"
	ts := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	first := state.TraceEvent{
		RunID: runID, Seq: 1, Timestamp: ts,
		Type: "run_started", ActorType: "agent", DataJSON: `{}`,
	}
	prev := GenesisHash(runID)
	h1, err := EventHash(first, prev)
	if err != nil {
		t.Fatal(err)
	}
	first.PrevHash = prev
	first.Hash = h1

	// Forged: appended with empty hash columns, so the genuine chain links past it.
	forged := state.TraceEvent{
		RunID: runID, Seq: 2, Timestamp: ts.Add(time.Second),
		Type: "tool_execution", ActorType: "agent", DataJSON: `{}`,
	}

	third := state.TraceEvent{
		RunID: runID, Seq: 3, Timestamp: ts.Add(2 * time.Second),
		Type: "run_finished", ActorType: "agent", DataJSON: `{}`,
	}
	h3, err := EventHash(third, h1)
	if err != nil {
		t.Fatal(err)
	}
	third.PrevHash = h1
	third.Hash = h3

	res := VerifyRunChain(runID, []state.TraceEvent{first, forged, third})
	if res.Ok() || res.BrokenSeq != 2 || res.BrokenField != BrokenFieldUnchainedInsertion {
		t.Fatalf("res=%+v", res)
	}
}

func TestVerifyRunChain_detectsTamperedHash(t *testing.T) {
	runID := "r1"
	ts := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	e := state.TraceEvent{
		RunID: runID, Seq: 1, Timestamp: ts,
		Type: "run_started", ActorType: "agent", DataJSON: `{}`,
		PrevHash: GenesisHash(runID),
		Hash:     "deadbeef",
	}
	res := VerifyRunChain(runID, []state.TraceEvent{e})
	if res.Ok() || res.BrokenSeq != 1 || res.BrokenField != BrokenFieldHash {
		t.Fatalf("res=%+v", res)
	}
}

func TestVerifyRunChain_detectsWrongPrevHash(t *testing.T) {
	runID := "r1"
	ts := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	e := state.TraceEvent{
		RunID: runID, Seq: 2, Timestamp: ts,
		Type: "run_finished", ActorType: "agent", DataJSON: `{}`,
		PrevHash: "not-genesis",
		Hash:     "abc123",
	}
	res := VerifyRunChain(runID, []state.TraceEvent{
		{RunID: runID, Seq: 1, Timestamp: ts, Type: "run_started", DataJSON: `{}`},
		e,
	})
	if res.Ok() || res.BrokenSeq != 2 || res.BrokenField != BrokenFieldPrevHash {
		t.Fatalf("res=%+v", res)
	}
}

func TestVerifyRunChain_detectsPartialChain(t *testing.T) {
	runID := "r1"
	e := state.TraceEvent{
		RunID: runID, Seq: 1, Timestamp: time.Now().UTC(),
		Type: "run_started", PrevHash: GenesisHash(runID),
	}
	res := VerifyRunChain(runID, []state.TraceEvent{e})
	if res.Ok() || res.BrokenField != BrokenFieldPartialChain {
		t.Fatalf("res=%+v", res)
	}
}

func TestVerifyRunChainError(t *testing.T) {
	if err := VerifyRunChainError("r1", nil); err != nil {
		t.Fatalf("empty chain: %v", err)
	}
	runID := "r1"
	e := state.TraceEvent{
		RunID: runID, Seq: 1, Timestamp: time.Now().UTC(),
		Type: "x", PrevHash: GenesisHash(runID), Hash: "bad",
	}
	if err := VerifyRunChainError(runID, []state.TraceEvent{e}); err == nil {
		t.Fatal("expected error")
	}
}
