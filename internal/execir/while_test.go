package execir

import (
	"context"
	"testing"
)

// whileProg builds `while <cond> limit N { step() }` where the body tool's result
// is bound to "s" so a stateful respond can drive the carried condition.
func whileProg(cond Expr, limit int) *Program {
	return &Program{
		Workflow: "w",
		Params:   []string{"input"},
		Body: []Node{
			&Let{Bind: "s", Value: Ref{Path: []string{"input"}}},
			&While{Cond: cond, Limit: limit, Body: []Node{
				&InvokeTool{Bind: "s", Uses: "step"},
			}},
		},
	}
}

// notDone is the condition `!s.done`.
var notDone = Not{X: Leaf{V: Ref{Path: []string{"s", "done"}}}}

func TestWhile_ConditionFalseInitially_ZeroExecutions(t *testing.T) {
	rec := &recorder{}
	prog := whileProg(notDone, 3)
	if _, err := (&Interp{Invoker: rec}).Run(context.Background(), prog, map[string]any{"done": true}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(rec.tools) != 0 {
		t.Fatalf("body ran %d times, want 0 (condition false initially)", len(rec.tools))
	}
}

func TestWhile_BecomesFalseAfterOneIteration(t *testing.T) {
	calls := 0
	rec := &recorder{respond: func(string, map[string]any) any {
		calls++
		return map[string]any{"done": true} // approved after the first body run
	}}
	prog := whileProg(notDone, 3)
	if _, err := (&Interp{Invoker: rec}).Run(context.Background(), prog, map[string]any{"done": false}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(rec.tools) != 1 {
		t.Fatalf("body ran %d times, want 1 (condition false after one iteration)", len(rec.tools))
	}
}

func TestWhile_ConditionStaysTrue_RunsExactlyLimit(t *testing.T) {
	rec := &recorder{respond: func(string, map[string]any) any {
		return map[string]any{"done": false} // never approves
	}}
	prog := whileProg(notDone, 3)
	if _, err := (&Interp{Invoker: rec}).Run(context.Background(), prog, map[string]any{"done": false}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(rec.tools) != 3 {
		t.Fatalf("body ran %d times, want exactly 3 (no fourth iteration on limit 3)", len(rec.tools))
	}
}

func TestWhile_GlobalCapStillProtectsRuntime(t *testing.T) {
	rec := &recorder{respond: func(string, map[string]any) any {
		return map[string]any{"done": false}
	}}
	prog := whileProg(notDone, 100) // source limit far above the global cap
	in := &Interp{Invoker: rec, MaxLoopIterations: 2}
	if _, err := in.Run(context.Background(), prog, map[string]any{"done": false}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(rec.tools) != 2 {
		t.Fatalf("body ran %d times, want 2 (effectiveMax = min(limit, global))", len(rec.tools))
	}
}

func TestWhile_CallSiteKeysDifferPerIteration(t *testing.T) {
	sr := &siteRecorder{}
	prog := whileProg(notDone, 3) // always-true → runs 3 iterations
	if _, err := (&Interp{Invoker: sr}).Run(context.Background(), prog, map[string]any{"done": false}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(sr.sites) != 3 {
		t.Fatalf("want 3 leaf invocations, got %d", len(sr.sites))
	}
	seen := map[string]bool{}
	for i, s := range sr.sites {
		k := CallKey(s)
		if seen[k] {
			t.Fatalf("iteration %d reused call key %q — memo replay would collide across iterations", i, k)
		}
		seen[k] = true
		if len(s.Loop) == 0 {
			t.Fatalf("iteration %d leaf has no loop index in its CallSite", i)
		}
	}
}

func TestWhile_DigestReflectsLimit(t *testing.T) {
	p3 := whileProg(notDone, 3)
	p4 := whileProg(notDone, 4)
	if p3.Digest() == p4.Digest() {
		t.Fatalf("digest ignores the limit: a limit change must invalidate a stale plan (#277)")
	}
}

func TestWhile_SerializeRoundTrip(t *testing.T) {
	prog := whileProg(notDone, 3)
	payload, err := MarshalPrograms(map[string]*Program{"w": prog})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalPrograms(payload)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["w"].Digest() != prog.Digest() {
		t.Fatalf("while did not survive serialize round-trip:\n got %s\nwant %s", got["w"].Digest(), prog.Digest())
	}
}
