package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/execir"
	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
)

func nativeTool(name string) *spec.ToolResource {
	return &spec.ToolResource{
		APIVersion: spec.APIVersionV0, Kind: spec.KindTool, Metadata: spec.Metadata{Name: name},
		Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(false)}},
	}
}

// cfGraph is a workflow whose FLATTENED resource projection contains both arms'
// steps (thn + els). If a control-flow run wrongly went through the DAG it would
// run both; the pinned program (execir) runs only the taken arm.
func cfGraph() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"thn":  nativeTool("thn"),
			"els":  nativeTool("els"),
			"each": nativeTool("each"),
		},
		Policies: map[string]*spec.PolicyResource{"default": {Spec: spec.PolicySpec{}}},
		Workflows: map[string]*spec.WorkflowResource{
			"cf": {
				APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "cf"},
				Spec: spec.WorkflowSpec{
					// Flattened arms (both), as the resource projection stores them.
					Steps: []spec.WorkflowStep{
						{ID: "a", Uses: "tool.thn.echo"},
						{ID: "b", Uses: "tool.els.echo"},
					},
					Output: &spec.WorkflowOutput{Value: map[string]any{"value": "${steps.a.output}"}},
				},
			},
		},
	}
}

func cfExecutor(t *testing.T, prog *execir.Program) (*Executor, *countingTools, string, time.Time) {
	t.Helper()
	ex, ct, runID, started := newResumeExecutor(t, cfGraph(), "cf")
	ex.Executables = map[string]*execir.Program{"cf": prog}
	return ex, ct, runID, started
}

func runCF(t *testing.T, ex *Executor, runID string, started time.Time, input map[string]any) *state.Run {
	t.Helper()
	// No UseExecIR: routing must send a control-flow program to the interpreter on its own.
	if err := ex.Run(context.Background(), RunInput{RunID: runID, WorkflowName: "cf", Env: "dev", StartedAt: started, Input: input}); err != nil {
		t.Fatalf("run: %v", err)
	}
	run, _ := ex.Store.GetRun(context.Background(), runID)
	return run
}

// TestControlFlow_BranchRunsOnlyTakenArm proves a control-flow workflow routes to
// the pinned program (not the flattened DAG): only the taken arm's tool runs.
func TestControlFlow_BranchRunsOnlyTakenArm(t *testing.T) {
	t.Parallel()
	prog := &execir.Program{Workflow: "cf", Params: []string{"input"}, Body: []execir.Node{
		&execir.Branch{
			Cond: execir.Leaf{V: execir.Ref{Path: []string{"input", "urgent"}}},
			Then: []execir.Node{&execir.InvokeTool{Bind: "a", Uses: "tool.thn.echo"}, &execir.Return{Value: execir.Ref{Path: []string{"a"}}}},
			Else: []execir.Node{&execir.InvokeTool{Bind: "b", Uses: "tool.els.echo"}, &execir.Return{Value: execir.Ref{Path: []string{"b"}}}},
		},
	}}

	// Taken THEN.
	ex, ct, runID, started := cfExecutor(t, prog)
	if run := runCF(t, ex, runID, started, map[string]any{"urgent": true}); run.Status != state.RunStatusSucceeded {
		t.Fatalf("then run status = %q err=%q", run.Status, run.ErrorText)
	}
	if ct.count("tool.thn.echo") != 1 || ct.count("tool.els.echo") != 0 {
		t.Fatalf("then arm: thn=%d els=%d, want 1/0 (flattened DAG would run both)", ct.count("tool.thn.echo"), ct.count("tool.els.echo"))
	}

	// Taken ELSE.
	ex2, ct2, runID2, started2 := cfExecutor(t, prog)
	if run := runCF(t, ex2, runID2, started2, map[string]any{"urgent": false}); run.Status != state.RunStatusSucceeded {
		t.Fatalf("else run status = %q err=%q", run.Status, run.ErrorText)
	}
	if ct2.count("tool.els.echo") != 1 || ct2.count("tool.thn.echo") != 0 {
		t.Fatalf("else arm: thn=%d els=%d, want 0/1", ct2.count("tool.thn.echo"), ct2.count("tool.els.echo"))
	}
}

// TestControlFlow_SequentialLoopEarlyReturn proves a sequential for with a return
// inside the body halts the loop and returns from the workflow.
func TestControlFlow_SequentialLoopEarlyReturn(t *testing.T) {
	t.Parallel()
	// for x in input.items { each(x); if x == "stop" { return x } }  — return halts.
	prog := &execir.Program{Workflow: "cf", Params: []string{"input"}, Body: []execir.Node{
		&execir.Loop{Var: "x", Collection: execir.Ref{Path: []string{"input", "items"}}, Body: []execir.Node{
			&execir.InvokeTool{Bind: "r", Uses: "tool.each.echo", Args: map[string]execir.Value{"v": execir.Ref{Path: []string{"x"}}}},
			&execir.Branch{
				Cond: execir.BinOp{Op: "==", X: execir.Leaf{V: execir.Ref{Path: []string{"x"}}}, Y: execir.Leaf{V: execir.Lit{V: "stop"}}},
				Then: []execir.Node{&execir.Return{Value: execir.Ref{Path: []string{"x"}}}},
			},
		}},
	}}
	ex, ct, runID, started := cfExecutor(t, prog)
	run := runCF(t, ex, runID, started, map[string]any{"items": []any{"a", "stop", "c"}})
	if run.Status != state.RunStatusSucceeded {
		t.Fatalf("status = %q err=%q", run.Status, run.ErrorText)
	}
	// each ran for "a" and "stop" (then the return halts before "c").
	if ct.count("tool.each.echo") != 2 {
		t.Fatalf("each should run twice before the early return, got %d", ct.count("tool.each.echo"))
	}
}

// TestControlFlow_ParallelForFanOut proves dynamic fan-out over a runtime
// collection runs one iteration per element.
func TestControlFlow_ParallelForFanOut(t *testing.T) {
	t.Parallel()
	prog := &execir.Program{Workflow: "cf", Params: []string{"input"}, Body: []execir.Node{
		&execir.Loop{Var: "x", Parallel: true, Collection: execir.Ref{Path: []string{"input", "items"}}, Body: []execir.Node{
			&execir.InvokeTool{Bind: "r", Uses: "tool.each.echo", Args: map[string]execir.Value{"v": execir.Ref{Path: []string{"x"}}}},
		}},
		&execir.Return{Value: execir.Lit{V: "done"}},
	}}
	ex, ct, runID, started := cfExecutor(t, prog)
	run := runCF(t, ex, runID, started, map[string]any{"items": []any{"a", "b", "c", "d"}})
	if run.Status != state.RunStatusSucceeded {
		t.Fatalf("status = %q err=%q", run.Status, run.ErrorText)
	}
	if ct.count("tool.each.echo") != 4 {
		t.Fatalf("parallel for should fan out to 4 iterations, got %d", ct.count("tool.each.echo"))
	}
}

// cfGatedGraph gates tool.publisher.echo via HITL interruptOn (mid-loop suspend).
func cfGatedGraph() *spec.ProjectGraph {
	g := cfGraph()
	g.Tools["publisher"] = &spec.ToolResource{APIVersion: spec.APIVersionV0, Kind: spec.KindTool, Metadata: spec.Metadata{Name: "publisher"}, Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(true)}}}
	g.Policies["gate"] = &spec.PolicyResource{Spec: spec.PolicySpec{
		Approvals: &spec.PolicyApprovals{RequiredFor: []string{"tool.publisher.echo"}},
		Hitl: &spec.HitlPolicy{InterruptOn: map[string]spec.HitlInterruptValue{
			"publisher": {Enabled: true, Config: &spec.HitlInterruptConfig{AllowedDecisions: []spec.HitlDecisionKind{spec.HitlDecisionApprove, spec.HitlDecisionReject}}},
		}},
	}}
	g.Workflows["cf"].Spec.Policy = "gate"
	// The flattened resource steps must cover the program's named binds (the loop
	// body's `e` and `p`), since the execir checkpoint records completed steps by
	// bind and validates them against the workflow's known step ids.
	g.Workflows["cf"].Spec.Steps = []spec.WorkflowStep{
		{ID: "e", Uses: "tool.each.echo"},
		{ID: "p", Uses: "tool.publisher.echo"},
	}
	return g
}

// TestControlFlow_ResumeMidLoop proves a suspend inside a loop iteration resumes
// from the pinned program without re-running completed iterations (#258 + #259).
func TestControlFlow_ResumeMidLoop(t *testing.T) {
	t.Parallel()
	prog := &execir.Program{Workflow: "cf", Params: []string{"input"}, Body: []execir.Node{
		&execir.Loop{Var: "x", Collection: execir.Ref{Path: []string{"input", "items"}}, Body: []execir.Node{
			&execir.InvokeTool{Bind: "e", Uses: "tool.each.echo", Args: map[string]execir.Value{"v": execir.Ref{Path: []string{"x"}}}},
			&execir.Branch{
				Cond: execir.BinOp{Op: "==", X: execir.Leaf{V: execir.Ref{Path: []string{"x"}}}, Y: execir.Leaf{V: execir.Lit{V: "gate"}}},
				Then: []execir.Node{&execir.InvokeTool{Bind: "p", Uses: "tool.publisher.echo", Args: map[string]execir.Value{"v": execir.Ref{Path: []string{"x"}}}}},
			},
		}},
		&execir.Return{Value: execir.Lit{V: "done"}},
	}}
	ex, ct, runID, started := newResumeExecutor(t, cfGatedGraph(), "cf")
	ex.Executables = map[string]*execir.Program{"cf": prog}
	ctx := context.Background()
	input := map[string]any{"items": []any{"a", "gate", "c"}}

	// Fresh: each runs for a and gate, publish suspends at the gate iteration.
	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "cf", Env: "dev", StartedAt: started, Input: input}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("fresh run should interrupt mid-loop, got %v", err)
	}
	if ct.count("tool.each.echo") != 2 || ct.count("tool.publisher.echo") != 0 {
		t.Fatalf("before suspend: each=%d publisher=%d, want 2/0", ct.count("tool.each.echo"), ct.count("tool.publisher.echo"))
	}

	// Resume approve: completed iterations (a, gate's each) replay from the memo —
	// NOT re-run — the gate resolves, and iteration c runs.
	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "cf", Env: "dev", StartedAt: started, Input: input,
		Resume: true, Hitl: HitlRunOptions{Actor: "alice", Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"}},
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if run, _ := ex.Store.GetRun(ctx, runID); run.Status != state.RunStatusSucceeded {
		t.Fatalf("resume status = %q err=%q", run.Status, run.ErrorText)
	}
	// each ran exactly 3 times total (a, gate fresh; c on resume) — a and gate were
	// NOT re-run (that would be 5); publisher ran once.
	if ct.count("tool.each.echo") != 3 {
		t.Fatalf("each ran %d times; completed iterations must replay from memo (want 3, not 5)", ct.count("tool.each.echo"))
	}
	if ct.count("tool.publisher.echo") != 1 {
		t.Fatalf("publisher should run once on resume, got %d", ct.count("tool.publisher.echo"))
	}
}

// nestedCFGraph is a STRAIGHT-LINE parent whose single workflow: step calls a
// control-flow child. The parent alone has no Branch/Loop, so an entry-only
// routing predicate would send the whole run to the DAG and execute both of the
// child's flattened arms (#259 review). Transitive routing must send it to execir.
func nestedCFGraph() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Tools:    map[string]*spec.ToolResource{"thn": nativeTool("thn"), "els": nativeTool("els")},
		Policies: map[string]*spec.PolicyResource{"default": {Spec: spec.PolicySpec{}}},
		Workflows: map[string]*spec.WorkflowResource{
			"parent": {
				APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "parent"},
				Spec: spec.WorkflowSpec{
					Steps:  []spec.WorkflowStep{{ID: "c", Workflow: "child"}},
					Output: &spec.WorkflowOutput{Value: map[string]any{"value": "${steps.c.output}"}},
				},
			},
			"child": {
				APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "child"},
				Spec: spec.WorkflowSpec{
					Steps:  []spec.WorkflowStep{{ID: "a", Uses: "tool.thn.echo"}, {ID: "b", Uses: "tool.els.echo"}},
					Output: &spec.WorkflowOutput{Value: map[string]any{"value": "${steps.a.output}"}},
				},
			},
		},
	}
}

// TestControlFlow_ChildBehindStraightLineParent proves the transitive routing fix:
// a control-flow child reached through a straight-line parent's workflow: step runs
// on the interpreter (only the taken arm), not the flattening DAG.
func TestControlFlow_ChildBehindStraightLineParent(t *testing.T) {
	t.Parallel()
	parentProg := &execir.Program{Workflow: "parent", Params: []string{"input"}, Body: []execir.Node{
		&execir.InvokeWorkflow{Bind: "c", Workflow: "child", Args: map[string]execir.Value{"urgent": execir.Ref{Path: []string{"input", "urgent"}}}},
		&execir.Return{Value: execir.Ref{Path: []string{"c"}}},
	}}
	childProg := &execir.Program{Workflow: "child", Params: []string{"input"}, Body: []execir.Node{
		&execir.Branch{
			Cond: execir.Leaf{V: execir.Ref{Path: []string{"input", "urgent"}}},
			Then: []execir.Node{&execir.InvokeTool{Bind: "a", Uses: "tool.thn.echo"}, &execir.Return{Value: execir.Ref{Path: []string{"a"}}}},
			Else: []execir.Node{&execir.InvokeTool{Bind: "b", Uses: "tool.els.echo"}, &execir.Return{Value: execir.Ref{Path: []string{"b"}}}},
		},
	}}
	execs := map[string]*execir.Program{"parent": parentProg, "child": childProg}

	ex, ct, runID, started := newResumeExecutor(t, nestedCFGraph(), "parent")
	ex.Executables = execs
	// No UseExecIR: transitive routing must detect the control-flow callee.
	if err := ex.Run(context.Background(), RunInput{RunID: runID, WorkflowName: "parent", Env: "dev", StartedAt: started, Input: map[string]any{"urgent": true}}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if run, _ := ex.Store.GetRun(context.Background(), runID); run.Status != state.RunStatusSucceeded {
		t.Fatalf("status = %q", run.Status)
	}
	if ct.count("tool.thn.echo") != 1 || ct.count("tool.els.echo") != 0 {
		t.Fatalf("nested child: thn=%d els=%d, want 1/0 (the DAG would run both arms)", ct.count("tool.thn.echo"), ct.count("tool.els.echo"))
	}
}

// TestControlFlow_ChildBehindParentResume proves the transitive-routed run also
// resumes correctly: a control-flow child suspends at a gate in the taken arm and,
// on resume, completes without running the untaken arm.
func TestControlFlow_ChildBehindParentResume(t *testing.T) {
	t.Parallel()
	g := nestedCFGraph()
	g.Tools["publisher"] = &spec.ToolResource{APIVersion: spec.APIVersionV0, Kind: spec.KindTool, Metadata: spec.Metadata{Name: "publisher"}, Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(true)}}}
	g.Policies["gate"] = &spec.PolicyResource{Spec: spec.PolicySpec{
		Approvals: &spec.PolicyApprovals{RequiredFor: []string{"tool.publisher.echo"}},
		Hitl: &spec.HitlPolicy{InterruptOn: map[string]spec.HitlInterruptValue{
			"publisher": {Enabled: true, Config: &spec.HitlInterruptConfig{AllowedDecisions: []spec.HitlDecisionKind{spec.HitlDecisionApprove, spec.HitlDecisionReject}}},
		}},
	}}
	g.Workflows["child"].Spec.Policy = "gate"
	g.Workflows["child"].Spec.Steps = []spec.WorkflowStep{{ID: "a", Uses: "tool.publisher.echo"}, {ID: "b", Uses: "tool.els.echo"}}
	g.Workflows["parent"].Spec.Policy = "gate"

	parentProg := &execir.Program{Workflow: "parent", Params: []string{"input"}, Body: []execir.Node{
		&execir.InvokeWorkflow{Bind: "c", Workflow: "child", Args: map[string]execir.Value{"urgent": execir.Ref{Path: []string{"input", "urgent"}}}},
		&execir.Return{Value: execir.Ref{Path: []string{"c"}}},
	}}
	childProg := &execir.Program{Workflow: "child", Params: []string{"input"}, Body: []execir.Node{
		&execir.Branch{
			Cond: execir.Leaf{V: execir.Ref{Path: []string{"input", "urgent"}}},
			Then: []execir.Node{&execir.InvokeTool{Bind: "a", Uses: "tool.publisher.echo"}, &execir.Return{Value: execir.Ref{Path: []string{"a"}}}},
			Else: []execir.Node{&execir.InvokeTool{Bind: "b", Uses: "tool.els.echo"}, &execir.Return{Value: execir.Ref{Path: []string{"b"}}}},
		},
	}}
	ex, ct, runID, started := newResumeExecutor(t, g, "parent")
	ex.Executables = map[string]*execir.Program{"parent": parentProg, "child": childProg}
	ctx := context.Background()

	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "parent", Env: "dev", StartedAt: started, Input: map[string]any{"urgent": true}}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("fresh run should interrupt at the child gate, got %v", err)
	}
	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "parent", Env: "dev", StartedAt: started, Input: map[string]any{"urgent": true},
		Resume: true, Hitl: HitlRunOptions{Actor: "alice", Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"}},
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if run, _ := ex.Store.GetRun(ctx, runID); run.Status != state.RunStatusSucceeded {
		t.Fatalf("resume status = %q err=%q", run.Status, run.ErrorText)
	}
	if ct.count("tool.publisher.echo") != 1 || ct.count("tool.els.echo") != 0 {
		t.Fatalf("resume: publisher=%d els=%d, want 1/0 (only the taken arm)", ct.count("tool.publisher.echo"), ct.count("tool.els.echo"))
	}
}
