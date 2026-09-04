package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/execir"
	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
	"github.com/Terfyn/terfyn/internal/tools"
	"github.com/Terfyn/terfyn/internal/trace"
)

// countingTools counts tool Call invocations per uses, so a resume test can prove
// a memoized leaf is not re-issued (no duplicate side effect).
type countingTools struct {
	inner tools.ToolExecutor
	mu    sync.Mutex
	calls map[string]int
}

func (c *countingTools) Call(ctx context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
	c.mu.Lock()
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[req.Uses]++
	c.mu.Unlock()
	return c.inner.Call(ctx, req)
}

func (c *countingTools) count(uses string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[uses]
}

// gatedTwoStepGraph is a sequential workflow: an ungated "prep" tool then a
// HITL-gated "publish" tool. On the execir path the run suspends at publish; a
// resume must NOT re-run prep.
func gatedTwoStepGraph() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"helper":    {APIVersion: spec.APIVersionV0, Kind: spec.KindTool, Metadata: spec.Metadata{Name: "helper"}, Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(false)}}},
			"publisher": {APIVersion: spec.APIVersionV0, Kind: spec.KindTool, Metadata: spec.Metadata{Name: "publisher"}, Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(true)}}},
		},
		Policies: map[string]*spec.PolicyResource{
			"gate": {Spec: spec.PolicySpec{
				Approvals: &spec.PolicyApprovals{RequiredFor: []string{"tool.publisher.echo"}},
				Hitl: &spec.HitlPolicy{InterruptOn: map[string]spec.HitlInterruptValue{
					"publisher": {Enabled: true, Config: &spec.HitlInterruptConfig{
						AllowedDecisions: []spec.HitlDecisionKind{spec.HitlDecisionApprove, spec.HitlDecisionReject},
					}},
				}},
			}},
		},
		Workflows: map[string]*spec.WorkflowResource{
			"pub": {
				APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "pub"},
				Spec: spec.WorkflowSpec{
					Policy: "gate",
					Steps: []spec.WorkflowStep{
						{ID: "prep", Uses: "tool.helper.echo", With: map[string]any{"topic": "${input.topic}"}},
						{ID: "publish", Uses: "tool.publisher.echo", With: map[string]any{"body": "${steps.prep.output.echo}"}},
					},
					Output: &spec.WorkflowOutput{Value: map[string]any{"published": "${steps.publish.output}"}},
				},
			},
		},
	}
}

func newResumeExecutor(t *testing.T, graph *spec.ProjectGraph, wf string) (*Executor, *countingTools, string, time.Time) {
	t.Helper()
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "resume.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	runID := "run-1"
	started := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{RunID: runID, WorkflowName: wf, Env: "dev", Status: state.RunStatusRunning, StartedAt: started, InputJSON: `{"topic":"hi"}`}); err != nil {
		t.Fatal(err)
	}
	ct := &countingTools{inner: tools.NewRegistry(graph)}
	ex := &Executor{Graph: graph, Tools: ct, Store: st, Trace: trace.NewRecorder(st), Now: func() time.Time { return started }}
	return ex, ct, runID, started
}

// TestExecIRResume_HitlGateNoDuplicateSideEffect is the core #258 acceptance: a
// run suspends at a uses: HITL gate, and resume completes without re-issuing the
// already-completed prep tool.
func TestExecIRResume_HitlGateNoDuplicateSideEffect(t *testing.T) {
	t.Parallel()
	ex, ct, runID, started := newResumeExecutor(t, gatedTwoStepGraph(), "pub")
	ctx := context.Background()

	// Fresh run on the execir path: prep runs, publish suspends.
	err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "pub", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"}})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("fresh run should interrupt at the gate, got %v", err)
	}
	if run, _ := ex.Store.GetRun(ctx, runID); run.Status != state.RunStatusInterrupted {
		t.Fatalf("status = %q", run.Status)
	}
	if got := ct.count("tool.helper.echo"); got != 1 {
		t.Fatalf("prep should have run once before suspend, got %d", got)
	}
	if got := ct.count("tool.publisher.echo"); got != 0 {
		t.Fatalf("gated publish must not run before approval, got %d", got)
	}

	// Resume with approve: prep must NOT re-run; publish runs; run succeeds.
	err = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "pub", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"},
		Resume: true, Hitl: HitlRunOptions{Actor: "alice", Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"}},
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if run, _ := ex.Store.GetRun(ctx, runID); run.Status != state.RunStatusSucceeded {
		t.Fatalf("resume status = %q err=%q", run.Status, run.ErrorText)
	}
	if got := ct.count("tool.helper.echo"); got != 1 {
		t.Fatalf("prep re-issued on resume (%d): memo replay must be side-effect-free", got)
	}
	if got := ct.count("tool.publisher.echo"); got != 1 {
		t.Fatalf("publish should run exactly once on resume, got %d", got)
	}
	assertTraceContains(t, ex.Store, runID, trace.EventHitlRequestCreated, trace.EventHitlDecisionSubmitted)
}

// TestExecIRResume_HitlReject aborts the run on a reject decision.
func TestExecIRResume_HitlReject(t *testing.T) {
	t.Parallel()
	ex, _, runID, started := newResumeExecutor(t, gatedTwoStepGraph(), "pub")
	ctx := context.Background()
	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "pub", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"}}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("fresh run should interrupt, got %v", err)
	}
	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "pub", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"},
		Resume: true, Hitl: HitlRunOptions{Actor: "alice", Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionReject, Actor: "alice"}},
	})
	if err == nil {
		t.Fatalf("reject should abort the run")
	}
	if run, _ := ex.Store.GetRun(ctx, runID); run.Status != state.RunStatusFailed {
		t.Fatalf("rejected run status = %q", run.Status)
	}
}

// TestExecIRResume_HitlGateInsideWhileLoop drives a bounded `while` (#290) through
// the engine with a HITL-gated tool in its body: each iteration runs a safe tool
// then a gated one that suspends. Across the two suspend/resume cycles it proves
// (a) the gate inside the loop suspends and resumes at the correct iteration, and
// (b) the pre-gate tool completed in an earlier iteration is replayed from the memo
// on resume, never re-issued — so its side effect fires exactly once per iteration.
func TestExecIRResume_HitlGateInsideWhileLoop(t *testing.T) {
	t.Parallel()
	graph := gatedTwoStepGraph()
	// The pinned execir program binds `pub` — deliberately NOT a WorkflowStep id
	// (the YAML step is `publish`), reproducing how an `.agent` control-flow leaf's
	// binding name differs from its flattened resource step id. Point the workflow
	// output at that bind so the output resolves to the loop's last-iteration result.
	graph.Workflows["pub"].Spec.Output = &spec.WorkflowOutput{Value: map[string]any{"done": "${steps.pub.output}"}}
	ex, ct, runID, started := newResumeExecutor(t, graph, "pub")
	ex.Executables = map[string]*execir.Program{
		"pub": {Workflow: "pub", Params: []string{"input"}, Body: []execir.Node{
			&execir.While{Cond: execir.Leaf{V: execir.Lit{V: true}}, Limit: 2, Body: []execir.Node{
				&execir.InvokeTool{Bind: "prep", Uses: "tool.helper.echo", Args: map[string]execir.Value{"topic": execir.Lit{V: "hi"}}},
				&execir.InvokeTool{Bind: "pub", Uses: "tool.publisher.echo", Args: map[string]execir.Value{"body": execir.Lit{V: "x"}}},
			}},
		}},
	}
	ctx := context.Background()
	base := RunInput{RunID: runID, WorkflowName: "pub", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"}}
	approve := func() RunInput {
		r := base
		r.Resume = true
		r.Hitl = HitlRunOptions{Actor: "alice", Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"}}
		return r
	}

	// Fresh: iteration 0's prep runs, then the gate suspends.
	if err := ex.Run(ctx, base); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("fresh run should interrupt at the gate, got %v", err)
	}
	if got := ct.count("tool.helper.echo"); got != 1 {
		t.Fatalf("iteration 0 prep should have run once, got %d", got)
	}
	if got := ct.count("tool.publisher.echo"); got != 0 {
		t.Fatalf("gated publish must not run before approval, got %d", got)
	}

	// Resume 1: iteration 0's gate completes; iteration 1's prep runs; gate suspends again.
	if err := ex.Run(ctx, approve()); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("resume 1 should suspend at iteration 1's gate, got %v", err)
	}
	if got := ct.count("tool.helper.echo"); got != 2 {
		t.Fatalf("prep fired %d times; want 2 (iteration 0 replayed from memo, iteration 1 ran once)", got)
	}
	if got := ct.count("tool.publisher.echo"); got != 1 {
		t.Fatalf("publisher fired %d times after resume 1; want 1 (iteration 0 only)", got)
	}

	// Resume 2: iteration 1's gate completes; the loop hits limit 2; the run succeeds.
	if err := ex.Run(ctx, approve()); err != nil {
		t.Fatalf("resume 2: %v", err)
	}
	if run, _ := ex.Store.GetRun(ctx, runID); run.Status != state.RunStatusSucceeded {
		t.Fatalf("final status = %q", run.Status)
	}
	if got := ct.count("tool.helper.echo"); got != 2 {
		t.Fatalf("prep re-issued (%d): memo replay across while iterations must be side-effect-free", got)
	}
	if got := ct.count("tool.publisher.echo"); got != 2 {
		t.Fatalf("publisher fired %d times; want exactly 2 (once per iteration, no fourth)", got)
	}
}

// approvalGraph is a sequential workflow with an approval node after a tool.
func approvalGraph() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Tools:    map[string]*spec.ToolResource{"helper": {APIVersion: spec.APIVersionV0, Kind: spec.KindTool, Metadata: spec.Metadata{Name: "helper"}, Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(false)}}}},
		Policies: map[string]*spec.PolicyResource{"default": {Spec: spec.PolicySpec{}}},
		Workflows: map[string]*spec.WorkflowResource{
			"appr": {
				APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "appr"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{
						{ID: "prep", Uses: "tool.helper.echo", With: map[string]any{"topic": "${input.topic}"}},
						{ID: "gate", Approval: &spec.WorkflowApprovalValue{Enabled: true}, With: map[string]any{"summary": "${steps.prep.output.echo}"}},
					},
					Output: &spec.WorkflowOutput{Value: map[string]any{"approved": "${steps.gate.output}"}},
				},
			},
		},
	}
}

// TestExecIRResume_ApprovalNode suspends at an Approval node and resumes to
// completion, publishing the reviewed payload.
func TestExecIRResume_ApprovalNode(t *testing.T) {
	t.Parallel()
	ex, ct, runID, started := newResumeExecutor(t, approvalGraph(), "appr")
	ctx := context.Background()
	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "appr", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"}}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("fresh run should interrupt at the approval node, got %v", err)
	}
	if got := ct.count("tool.helper.echo"); got != 1 {
		t.Fatalf("prep should run once before the approval, got %d", got)
	}
	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "appr", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"},
		Resume: true, Hitl: HitlRunOptions{Actor: "alice", Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"}},
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if run, _ := ex.Store.GetRun(ctx, runID); run.Status != state.RunStatusSucceeded {
		t.Fatalf("resume status = %q err=%q", run.Status, run.ErrorText)
	}
	if got := ct.count("tool.helper.echo"); got != 1 {
		t.Fatalf("prep re-issued on resume (%d)", got)
	}
}

// perBranchGraph is a needs-DAG: an ungated "sib" root and a HITL-gated "publish"
// root run concurrently. The gate suspends the run; the sibling completes and is
// memoized. (#270 per-branch suspend, #192/#195.)
func perBranchGraph() *spec.ProjectGraph {
	g := gatedTwoStepGraph()
	wf := g.Workflows["pub"]
	// sib is a root; publish needs sib, so sib deterministically completes (and is
	// memoized) before publish suspends. The graph still executes through execGraph
	// (explicit needs), so this exercises the Graph suspend + memo-replay path; the
	// purely-independent concurrent case is covered by the interpreter unit test
	// TestDurable_GraphPerBranchSuspend (whose stub ignores cancellation, so the
	// sibling always completes deterministically).
	wf.Spec.Steps = []spec.WorkflowStep{
		{ID: "sib", Uses: "tool.helper.echo", With: map[string]any{"topic": "${input.topic}"}, NeedsDeclared: true},
		{ID: "publish", Uses: "tool.publisher.echo", With: map[string]any{"x": "1"}, Needs: []string{"sib"}, NeedsDeclared: true},
	}
	wf.Spec.Output = &spec.WorkflowOutput{Value: map[string]any{
		"sib":       "${steps.sib.output}",
		"published": "${steps.publish.output}",
	}}
	return g
}

// TestExecIRResume_GraphPerBranchSuspend proves a gate in one graph branch
// suspends the run while the independent sibling completes, and resume runs
// neither the sibling again nor duplicates its side effect.
func TestExecIRResume_GraphPerBranchSuspend(t *testing.T) {
	t.Parallel()
	ex, ct, runID, started := newResumeExecutor(t, perBranchGraph(), "pub")
	ctx := context.Background()
	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "pub", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"}}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("fresh run should interrupt at the gate, got %v", err)
	}
	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "pub", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"},
		Resume: true, Hitl: HitlRunOptions{Actor: "alice", Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"}},
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if run, _ := ex.Store.GetRun(ctx, runID); run.Status != state.RunStatusSucceeded {
		t.Fatalf("resume status = %q err=%q", run.Status, run.ErrorText)
	}
	// The independent sibling ran exactly once across suspend+resume (memo replay),
	// and the gated branch ran exactly once (on resume).
	if got := ct.count("tool.helper.echo"); got != 1 {
		t.Fatalf("sibling should run exactly once total, got %d", got)
	}
	if got := ct.count("tool.publisher.echo"); got != 1 {
		t.Fatalf("gated branch should run once on resume, got %d", got)
	}
}

// twoConcurrentGateGraph is a needs-DAG whose two independent roots each call a
// distinct HITL-gated tool, so a single run reaches two independent gates in one
// concurrent group (issue #275).
func twoConcurrentGateGraph() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"pub1": {APIVersion: spec.APIVersionV0, Kind: spec.KindTool, Metadata: spec.Metadata{Name: "pub1"}, Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(true)}}},
			"pub2": {APIVersion: spec.APIVersionV0, Kind: spec.KindTool, Metadata: spec.Metadata{Name: "pub2"}, Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(true)}}},
		},
		Policies: map[string]*spec.PolicyResource{
			"gate": {Spec: spec.PolicySpec{
				Approvals: &spec.PolicyApprovals{RequiredFor: []string{"tool.pub1.echo", "tool.pub2.echo"}},
				Hitl: &spec.HitlPolicy{InterruptOn: map[string]spec.HitlInterruptValue{
					"pub1": {Enabled: true, Config: &spec.HitlInterruptConfig{AllowedDecisions: []spec.HitlDecisionKind{spec.HitlDecisionApprove, spec.HitlDecisionReject}}},
					"pub2": {Enabled: true, Config: &spec.HitlInterruptConfig{AllowedDecisions: []spec.HitlDecisionKind{spec.HitlDecisionApprove, spec.HitlDecisionReject}}},
				}},
			}},
		},
		Workflows: map[string]*spec.WorkflowResource{
			"twogate": {
				APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "twogate"},
				Spec: spec.WorkflowSpec{
					Policy: "gate",
					Steps: []spec.WorkflowStep{
						{ID: "a", Uses: "tool.pub1.echo", With: map[string]any{"x": "1"}, NeedsDeclared: true},
						{ID: "b", Uses: "tool.pub2.echo", With: map[string]any{"x": "2"}, NeedsDeclared: true},
					},
					Output: &spec.WorkflowOutput{Value: map[string]any{"a": "${steps.a.output}", "b": "${steps.b.output}"}},
				},
			},
		},
	}
}

// TestExecIRResume_TwoConcurrentGates is the #275 acceptance: two independent HITL
// gates in one concurrent group resume to completion across two decisions — the
// second gate re-suspends on the first resume instead of failing closed (exit 5),
// and neither side effect fires twice. First-wins means one gate is presented per
// suspend; which of a/b wins the fresh-run slot is a race, so the assertions read
// the aggregate, not a fixed branch.
func TestExecIRResume_TwoConcurrentGates(t *testing.T) {
	t.Parallel()
	ex, ct, runID, started := newResumeExecutor(t, twoConcurrentGateGraph(), "twogate")
	ctx := context.Background()
	approve := HitlRunOptions{Actor: "alice", Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"}}

	// Fresh run: both branches reach their gate; one wins the slot and the run
	// suspends. No gated tool runs before approval.
	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "twogate", Env: "dev", StartedAt: started, Input: map[string]any{}}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("fresh run should interrupt at the first gate, got %v", err)
	}
	if got := ct.count("tool.pub1.echo") + ct.count("tool.pub2.echo"); got != 0 {
		t.Fatalf("no gated tool should run before approval, got %d", got)
	}

	// Resume 1: approve the presented gate. It must re-suspend at the SECOND gate,
	// not fail closed and not complete (issue #275).
	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "twogate", Env: "dev", StartedAt: started, Input: map[string]any{}, Resume: true, Hitl: approve}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("resume 1 should re-suspend at the second gate, got %v", err)
	}
	if run, _ := ex.Store.GetRun(ctx, runID); run.Status != state.RunStatusInterrupted {
		t.Fatalf("resume 1 status = %q err=%q (want interrupted, not fail-closed)", run.Status, run.ErrorText)
	}
	if got := ct.count("tool.pub1.echo") + ct.count("tool.pub2.echo"); got != 1 {
		t.Fatalf("exactly one gated tool should have run after the first approval, got %d", got)
	}

	// Resume 2: approve the second gate. The run completes; the first branch is
	// replayed from the memo (no duplicate), so each gated tool ran exactly once.
	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "twogate", Env: "dev", StartedAt: started, Input: map[string]any{}, Resume: true, Hitl: approve}); err != nil {
		t.Fatalf("resume 2: %v", err)
	}
	if run, _ := ex.Store.GetRun(ctx, runID); run.Status != state.RunStatusSucceeded {
		t.Fatalf("resume 2 status = %q err=%q", run.Status, run.ErrorText)
	}
	if got := ct.count("tool.pub1.echo"); got != 1 {
		t.Fatalf("pub1 should run exactly once total, got %d", got)
	}
	if got := ct.count("tool.pub2.echo"); got != 1 {
		t.Fatalf("pub2 should run exactly once total, got %d", got)
	}
}

// TestExecIRResume_TwoConcurrentGates_RejectAborts proves a reject on one gate of
// a concurrent group aborts the run even though the sibling gate is still
// suspended: siblings are no longer cancelled by a suspend (#275), so the reject
// is a real fault that must win over the concurrent pause, not be masked by it.
func TestExecIRResume_TwoConcurrentGates_RejectAborts(t *testing.T) {
	t.Parallel()
	ex, ct, runID, started := newResumeExecutor(t, twoConcurrentGateGraph(), "twogate")
	ctx := context.Background()
	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "twogate", Env: "dev", StartedAt: started, Input: map[string]any{}}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("fresh run should interrupt, got %v", err)
	}
	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "twogate", Env: "dev", StartedAt: started, Input: map[string]any{},
		Resume: true, Hitl: HitlRunOptions{Actor: "alice", Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionReject, Actor: "alice"}},
	})
	if err == nil || errors.Is(err, ErrInterrupted) {
		t.Fatalf("reject on one concurrent gate should abort the run, got %v", err)
	}
	if run, _ := ex.Store.GetRun(ctx, runID); run.Status != state.RunStatusFailed {
		t.Fatalf("rejected run status = %q (want failed)", run.Status)
	}
	if got := ct.count("tool.pub1.echo") + ct.count("tool.pub2.echo"); got != 0 {
		t.Fatalf("no gated tool should run when a gate is rejected, got %d", got)
	}
}

// directGateWithSubworkflowGraph is a needs-DAG whose two concurrent roots are a
// direct HITL gate ("g") and a subworkflow call ("w") whose callee runs an
// ungated step ("innerprep") then an inner HITL gate ("innergate"). It exercises
// the interaction of the #275 no-cancel change with the single suspension slot:
// the direct gate can win the slot while the child has already committed its
// inner step (issue #275 review).
func directGateWithSubworkflowGraph() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"helper": {APIVersion: spec.APIVersionV0, Kind: spec.KindTool, Metadata: spec.Metadata{Name: "helper"}, Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(false)}}},
			"pub1":   {APIVersion: spec.APIVersionV0, Kind: spec.KindTool, Metadata: spec.Metadata{Name: "pub1"}, Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(true)}}},
			"pub2":   {APIVersion: spec.APIVersionV0, Kind: spec.KindTool, Metadata: spec.Metadata{Name: "pub2"}, Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(true)}}},
		},
		Policies: map[string]*spec.PolicyResource{
			"gate": {Spec: spec.PolicySpec{
				Approvals: &spec.PolicyApprovals{RequiredFor: []string{"tool.pub1.echo", "tool.pub2.echo"}},
				Hitl: &spec.HitlPolicy{InterruptOn: map[string]spec.HitlInterruptValue{
					"pub1": {Enabled: true, Config: &spec.HitlInterruptConfig{AllowedDecisions: []spec.HitlDecisionKind{spec.HitlDecisionApprove, spec.HitlDecisionReject}}},
					"pub2": {Enabled: true, Config: &spec.HitlInterruptConfig{AllowedDecisions: []spec.HitlDecisionKind{spec.HitlDecisionApprove, spec.HitlDecisionReject}}},
				}},
			}},
		},
		Workflows: map[string]*spec.WorkflowResource{
			"child": {
				APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "child"},
				Spec: spec.WorkflowSpec{
					Policy: "gate",
					Steps: []spec.WorkflowStep{
						{ID: "innerprep", Uses: "tool.helper.echo", With: map[string]any{"topic": "${input.topic}"}},
						{ID: "innergate", Uses: "tool.pub2.echo", With: map[string]any{"y": "${steps.innerprep.output.echo.topic}"}},
					},
					Output: &spec.WorkflowOutput{Value: map[string]any{"r": "${steps.innergate.output}"}},
				},
			},
			"parent": {
				APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "parent"},
				Spec: spec.WorkflowSpec{
					Policy: "gate",
					Steps: []spec.WorkflowStep{
						{ID: "g", Uses: "tool.pub1.echo", With: map[string]any{"x": "1"}, NeedsDeclared: true},
						{ID: "w", Workflow: "child", With: map[string]any{"topic": "hi"}, NeedsDeclared: true},
					},
					Output: &spec.WorkflowOutput{Value: map[string]any{"g": "${steps.g.output}", "w": "${steps.w.output}"}},
				},
			},
		},
	}
}

// TestExecIRResume_DirectGateWithConcurrentSubworkflow proves that a subworkflow
// running concurrently with a direct gate does not re-run its already-committed
// inner step on resume, even when the direct gate wins the single suspension slot
// (issue #275 review, SOUNDNESS.md S7). Resolving every gate over successive
// resumes must run each tool exactly once.
func TestExecIRResume_DirectGateWithConcurrentSubworkflow(t *testing.T) {
	t.Parallel()
	ex, ct, runID, started := newResumeExecutor(t, directGateWithSubworkflowGraph(), "parent")
	ctx := context.Background()
	approve := HitlRunOptions{Actor: "alice", Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"}}

	err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "parent", Env: "dev", StartedAt: started, Input: map[string]any{}})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("fresh run should interrupt, got %v", err)
	}
	// Resolve every outstanding gate across successive resumes (bounded).
	for i := 0; i < 5 && errors.Is(err, ErrInterrupted); i++ {
		err = ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "parent", Env: "dev", StartedAt: started, Input: map[string]any{}, Resume: true, Hitl: approve})
	}
	if err != nil {
		t.Fatalf("run did not complete across resumes: %v", err)
	}
	if run, _ := ex.Store.GetRun(ctx, runID); run.Status != state.RunStatusSucceeded {
		t.Fatalf("status = %q err=%q", run.Status, run.ErrorText)
	}
	if got := ct.count("tool.helper.echo"); got != 1 {
		t.Fatalf("inner prep re-ran on resume (%d): a committed inner step must not fire twice (S7)", got)
	}
	if got := ct.count("tool.pub1.echo"); got != 1 {
		t.Fatalf("direct gate tool should run exactly once, got %d", got)
	}
	if got := ct.count("tool.pub2.echo"); got != 1 {
		t.Fatalf("inner gate tool should run exactly once, got %d", got)
	}
}

// nestedSubworkflowGraph is a parent workflow whose only step calls a child that
// has an ungated step then a HITL-gated step.
func nestedSubworkflowGraph() *spec.ProjectGraph {
	g := gatedTwoStepGraph()
	inner := &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "inner"},
		Spec: spec.WorkflowSpec{
			Policy: "gate",
			Steps: []spec.WorkflowStep{
				{ID: "prep", Uses: "tool.helper.echo", With: map[string]any{"topic": "${input.topic}"}},
				{ID: "gatestep", Uses: "tool.publisher.echo", With: map[string]any{"body": "${steps.prep.output.echo}"}},
			},
			Output: &spec.WorkflowOutput{Value: map[string]any{"r": "${steps.gatestep.output}"}},
		},
	}
	outer := &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "outer"},
		Spec: spec.WorkflowSpec{
			Policy: "gate",
			Steps:  []spec.WorkflowStep{{ID: "sub", Workflow: "inner", With: map[string]any{"topic": "${input.topic}"}}},
			Output: &spec.WorkflowOutput{Value: map[string]any{"result": "${steps.sub.output}"}},
		},
	}
	g.Workflows = map[string]*spec.WorkflowResource{"inner": inner, "outer": outer}
	return g
}

// TestExecIRResume_NestedSubworkflow proves a workflow: step whose callee suspends
// at an inner gate resumes without re-running completed inner steps (#194/#270).
func TestExecIRResume_NestedSubworkflow(t *testing.T) {
	t.Parallel()
	ex, ct, runID, started := newResumeExecutor(t, nestedSubworkflowGraph(), "outer")
	ctx := context.Background()
	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "outer", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"}}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("fresh run should interrupt at the inner gate, got %v", err)
	}
	if got := ct.count("tool.helper.echo"); got != 1 {
		t.Fatalf("inner prep should run once before suspend, got %d", got)
	}
	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "outer", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"},
		Resume: true, Hitl: HitlRunOptions{Actor: "alice", Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"}},
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if run, _ := ex.Store.GetRun(ctx, runID); run.Status != state.RunStatusSucceeded {
		t.Fatalf("resume status = %q err=%q", run.Status, run.ErrorText)
	}
	if got := ct.count("tool.helper.echo"); got != 1 {
		t.Fatalf("inner prep re-run on resume (%d): completed inner step must replay from the nested memo", got)
	}
	if got := ct.count("tool.publisher.echo"); got != 1 {
		t.Fatalf("inner gated step should run once on resume, got %d", got)
	}
}

// twoLevelNestedGraph nests the gated inner workflow one level deeper:
// outer → mid → inner, with the HITL gate in inner. mid is a pure pass-through
// (a single workflow: step calling inner), so the gate lives two subworkflow
// levels below the run root.
func twoLevelNestedGraph() *spec.ProjectGraph {
	g := nestedSubworkflowGraph()
	mid := &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "mid"},
		Spec: spec.WorkflowSpec{
			Policy: "gate",
			Steps:  []spec.WorkflowStep{{ID: "sub", Workflow: "inner", With: map[string]any{"topic": "${input.topic}"}}},
			Output: &spec.WorkflowOutput{Value: map[string]any{"result": "${steps.sub.output}"}},
		},
	}
	g.Workflows["mid"] = mid
	g.Workflows["outer"].Spec.Steps[0].Workflow = "mid"
	return g
}

// TestExecIRResume_TwoLevelNestedSubworkflow proves a HITL gate two subworkflow
// levels deep (outer→mid→inner) resumes: the decision reaches the innermost gate,
// each level's completed pre-gate steps replay from its memo, and the run succeeds
// after a single approve — instead of re-running inner fresh on every resume and
// re-suspending at the same gate forever (S7, issue #380).
func TestExecIRResume_TwoLevelNestedSubworkflow(t *testing.T) {
	t.Parallel()
	ex, ct, runID, started := newResumeExecutor(t, twoLevelNestedGraph(), "outer")
	ctx := context.Background()
	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "outer", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"}}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("fresh run should interrupt at the inner gate, got %v", err)
	}
	if got := ct.count("tool.helper.echo"); got != 1 {
		t.Fatalf("inner prep should run once before suspend, got %d", got)
	}
	if got := ct.count("tool.publisher.echo"); got != 0 {
		t.Fatalf("gated publisher must not run before approval, got %d", got)
	}

	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "outer", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"},
		Resume: true, Hitl: HitlRunOptions{Actor: "alice", Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"}},
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if run, _ := ex.Store.GetRun(ctx, runID); run.Status != state.RunStatusSucceeded {
		t.Fatalf("resume status = %q err=%q (a two-level nested gate must resume, not re-suspend forever)", run.Status, run.ErrorText)
	}
	if got := ct.count("tool.helper.echo"); got != 1 {
		t.Fatalf("inner prep re-run on resume (%d): completed inner step must replay from the nested memo, not re-execute", got)
	}
	if got := ct.count("tool.publisher.echo"); got != 1 {
		t.Fatalf("inner gated step should run exactly once after approval, got %d", got)
	}
}

// TestExecIRResume_HitlGateInsideWhileInsideSubworkflow is the nested counterpart
// of TestExecIRResume_HitlGateInsideWhileLoop (#290): the bounded `while` with a
// gated body runs inside a `workflow:` callee. It carries through TWO suspend/resume
// cycles so the gated leaf `pub` completes inside the nested frame (resume 1) and
// the NEXT load (resume 2) revalidates that frame — the point where the ungated
// nested DAG-era Steps-membership check used to reject a non-step bind. Proves the
// nested relaxation and that the inner pre-gate tool replays per iteration.
func TestExecIRResume_HitlGateInsideWhileInsideSubworkflow(t *testing.T) {
	t.Parallel()
	graph := nestedSubworkflowGraph()
	// The inner pinned program binds `pub` (NOT the callee's `gatestep` step id),
	// reproducing the control-flow bind/step-id divergence one layer down. Point the
	// inner output at that bind.
	graph.Workflows["inner"].Spec.Output = &spec.WorkflowOutput{Value: map[string]any{"done": "${steps.pub.output}"}}
	ex, ct, runID, started := newResumeExecutor(t, graph, "outer")
	ex.Executables = map[string]*execir.Program{
		"outer": {Workflow: "outer", Params: []string{"input"}, Body: []execir.Node{
			&execir.InvokeWorkflow{Bind: "sub", Workflow: "inner", Args: map[string]execir.Value{"topic": execir.Lit{V: "hi"}}},
		}},
		"inner": {Workflow: "inner", Params: []string{"input"}, Body: []execir.Node{
			&execir.While{Cond: execir.Leaf{V: execir.Lit{V: true}}, Limit: 2, Body: []execir.Node{
				&execir.InvokeTool{Bind: "prep", Uses: "tool.helper.echo", Args: map[string]execir.Value{"topic": execir.Lit{V: "hi"}}},
				&execir.InvokeTool{Bind: "pub", Uses: "tool.publisher.echo", Args: map[string]execir.Value{"body": execir.Lit{V: "x"}}},
			}},
		}},
	}
	ctx := context.Background()
	base := RunInput{RunID: runID, WorkflowName: "outer", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"}}
	approve := func() RunInput {
		r := base
		r.Resume = true
		r.Hitl = HitlRunOptions{Actor: "alice", Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"}}
		return r
	}

	// Fresh: inner iteration 0 prep runs, gate suspends.
	if err := ex.Run(ctx, base); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("fresh run should interrupt at the inner gate, got %v", err)
	}
	if got := ct.count("tool.helper.echo"); got != 1 {
		t.Fatalf("inner iteration 0 prep should run once, got %d", got)
	}

	// Resume 1: inner iteration 0's gate completes (nested Steps gains `pub`); inner
	// iteration 1's prep runs; gate suspends again. The next load (resume 2)
	// revalidates this nested frame.
	if err := ex.Run(ctx, approve()); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("resume 1 should suspend at inner iteration 1's gate, got %v", err)
	}
	if got := ct.count("tool.helper.echo"); got != 2 {
		t.Fatalf("inner prep fired %d times; want 2 (iteration 0 replayed from the nested memo, iteration 1 ran)", got)
	}
	if got := ct.count("tool.publisher.echo"); got != 1 {
		t.Fatalf("inner publisher fired %d times after resume 1; want 1", got)
	}

	// Resume 2: inner iteration 1's gate completes; the loop hits limit 2; the run succeeds.
	if err := ex.Run(ctx, approve()); err != nil {
		t.Fatalf("resume 2: %v", err)
	}
	if run, _ := ex.Store.GetRun(ctx, runID); run.Status != state.RunStatusSucceeded {
		t.Fatalf("final status = %q err=%q", run.Status, run.ErrorText)
	}
	if got := ct.count("tool.helper.echo"); got != 2 {
		t.Fatalf("inner prep re-issued (%d): nested memo replay across while iterations must be side-effect-free", got)
	}
	if got := ct.count("tool.publisher.echo"); got != 2 {
		t.Fatalf("inner publisher fired %d times; want exactly 2 (once per iteration)", got)
	}
}

// TestExecIR_UsesPinnedProgram proves runViaExecIR executes the PINNED program
// (Executor.Executables) rather than re-lowering the resource graph (issue #260):
// a sentinel program that invokes a different tool than the workflow's steps runs
// that tool, not the workflow's.
func TestExecIR_UsesPinnedProgram(t *testing.T) {
	t.Parallel()
	graph := gatedTwoStepGraph()
	// Replace the workflow with a simple single-step one that uses helper.echo.
	graph.Workflows["pub"].Spec = spec.WorkflowSpec{
		Policy: "gate",
		Steps:  []spec.WorkflowStep{{ID: "a", Uses: "tool.helper.echo", With: map[string]any{"x": "1"}}},
		Output: &spec.WorkflowOutput{Value: map[string]any{"r": "${steps.a.output}"}},
	}
	// Add a sentinel tool the pinned program will call instead.
	graph.Tools["sentinel"] = &spec.ToolResource{APIVersion: spec.APIVersionV0, Kind: spec.KindTool, Metadata: spec.Metadata{Name: "sentinel"}, Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(false)}}}

	ex, ct, runID, started := newResumeExecutor(t, graph, "pub")
	ex.Executables = map[string]*execir.Program{
		"pub": {Workflow: "pub", Params: []string{"input"}, Body: []execir.Node{
			&execir.InvokeTool{Bind: "a", Uses: "tool.sentinel.echo"},
			&execir.Return{Value: execir.Ref{Path: []string{"a"}}},
		}},
	}
	if err := ex.Run(context.Background(), RunInput{RunID: runID, WorkflowName: "pub", Env: "dev", StartedAt: started, Input: map[string]any{}}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if ct.count("tool.sentinel.echo") != 1 {
		t.Fatalf("pinned program's sentinel tool should have run once, got %d", ct.count("tool.sentinel.echo"))
	}
	if ct.count("tool.helper.echo") != 0 {
		t.Fatalf("the workflow's own step must NOT run — the pinned program is authority, got %d", ct.count("tool.helper.echo"))
	}
}

// TestExecIRResume_DagCheckpointRoutesToDag guards backward compatibility: a run
// on the DAG path writes a non-execir checkpoint, so resumeIsExecIR is false and
// resume stays on the DAG path.
// TestExecIRResume_LegacyDagCheckpointFailsLoudly proves the #278 hard format cut:
// a pre-execir (WorkflowStep DAG) checkpoint — one without the ExecIR marker — is
// not resumable. Resume fails loudly rather than routing to a runtime that no
// longer exists; there is no DAG→execir migration.
func TestExecIRResume_LegacyDagCheckpointFailsLoudly(t *testing.T) {
	t.Parallel()
	ex, _, runID, started := newResumeExecutor(t, hitlTestGraph(), "hitl")
	ctx := context.Background()
	// Seed a legacy DAG-shaped interrupted checkpoint (no execIR marker).
	if err := ex.Store.SaveCheckpoint(ctx, state.RunCheckpoint{
		RunID: runID, StepIndex: 0, StepID: "gate",
		ContextJSON: `{"version":1,"input":{},"steps":{},"totalCostUsd":0,"pendingHitl":{"stepId":"gate"}}`,
		Status:      state.CheckpointStatusInterrupted, CreatedAt: started,
	}); err != nil {
		t.Fatal(err)
	}
	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "dev", StartedAt: started, Input: map[string]any{},
		Resume: true, Hitl: HitlRunOptions{Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "a"}},
	})
	if err == nil || errors.Is(err, ErrInterrupted) {
		t.Fatalf("legacy DAG checkpoint must fail resume loudly, got %v", err)
	}
	if !strings.Contains(err.Error(), "not resumable") {
		t.Fatalf("want a clear not-resumable error, got %v", err)
	}
}
