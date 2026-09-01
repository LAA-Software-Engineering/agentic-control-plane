package execir

import (
	"context"
	"testing"
)

// TestDurable_WhileMemoReplayAcrossIterations is the J5 (#290) core guarantee for
// bounded `while`: a leaf that COMPLETED in an earlier iteration is replayed from
// the memo on resume, never re-invoked, so its side effect fires exactly once even
// though the run suspended inside the loop. Iteration i folds into the leaf
// CallSite.Loop vector, so iteration 0's `a` and iteration 1's `a` have distinct
// memo keys and do not collide.
func TestDurable_WhileMemoReplayAcrossIterations(t *testing.T) {
	t.Parallel()
	prog := &Program{Workflow: "W", Body: []Node{
		&While{Cond: Leaf{V: Lit{V: true}}, Limit: 2, Body: []Node{
			&InvokeTool{Bind: "a", Uses: "tool.t.a"},
			&InvokeTool{Bind: "g", Uses: "tool.t.gate"},
		}},
	}}
	stub := &durableStub{suspendUses: "tool.t.gate"}
	in := &Interp{Invoker: stub}

	// Fresh run: iteration 0 runs `a` (completes, memoized) then the gate suspends.
	_, st, err := in.RunResumable(context.Background(), prog, nil, nil)
	if err != nil {
		t.Fatalf("fresh run: %v", err)
	}
	if !st.Suspended {
		t.Fatalf("expected suspension at the gate inside the loop")
	}
	if stub.calls["tool.t.a"] != 1 {
		t.Fatalf("after suspend: a=%d, want 1", stub.calls["tool.t.a"])
	}

	// Resume: iteration 0's `a` MUST be replayed (not reissued); the gate completes;
	// iteration 1 then runs `a` a second time. So `a` fires exactly twice total —
	// once per iteration — never three times.
	_, st2, err := in.RunResumable(context.Background(), prog, nil, st)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if st2.Suspended {
		t.Fatalf("resume should complete, not re-suspend")
	}
	if stub.calls["tool.t.a"] != 2 {
		t.Fatalf("tool a fired %d times; want exactly 2 (iteration 0 replayed from memo, not reissued)", stub.calls["tool.t.a"])
	}
}

// TestDurable_WhileConditionReplay proves the per-iteration `while` condition is
// recorded and verified across resume: an identical replay of a condition driven
// by carried state passes (each iteration's decision is keyed by path AND
// iteration, so iteration N is not misread as a divergence from N-1).
func TestDurable_WhileConditionReplay(t *testing.T) {
	t.Parallel()
	prog := &Program{Workflow: "W", Params: []string{"input"}, Body: []Node{
		&While{Cond: Leaf{V: Ref{Path: []string{"input", "go"}}}, Limit: 3, Body: []Node{
			&InvokeTool{Uses: "tool.t.step"},
		}},
	}}
	in := &Interp{Invoker: &recorder{}}
	input := map[string]any{"go": true} // constant → runs the full bound of 3
	_, st, err := in.RunResumable(context.Background(), prog, input, nil)
	if err != nil {
		t.Fatalf("fresh run: %v", err)
	}
	// Replay with the same input: every iteration's recorded condition must verify.
	if _, _, err := in.RunResumable(context.Background(), prog, input, st); err != nil {
		t.Fatalf("identical replay of the while condition must pass: %v", err)
	}
}

// TestDurable_WhileConditionDivergesFailsLoudly proves a non-deterministic while
// condition that flips between the original run and the replay is a LOUD error,
// not a silently different iteration history (Part 4 of the epic).
func TestDurable_WhileConditionDivergesFailsLoudly(t *testing.T) {
	t.Parallel()
	prog := &Program{Workflow: "W", Params: []string{"input"}, Body: []Node{
		&While{Cond: Leaf{V: Ref{Path: []string{"input", "go"}}}, Limit: 3, Body: []Node{
			&InvokeTool{Uses: "tool.t.step"},
		}},
	}}
	in := &Interp{Invoker: &recorder{}}
	_, st, err := in.RunResumable(context.Background(), prog, map[string]any{"go": true}, nil)
	if err != nil {
		t.Fatalf("fresh run: %v", err)
	}
	// Resume with the condition flipped: iteration 0 was recorded true; now false.
	_, _, err = in.RunResumable(context.Background(), prog, map[string]any{"go": false}, st)
	if err == nil {
		t.Fatalf("expected a determinism-violation error when the while condition diverges on replay")
	}
}
