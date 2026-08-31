package engine

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/terfyn/internal/policy"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
	"github.com/LAA-Software-Engineering/terfyn/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/terfyn/internal/tools"
	"github.com/LAA-Software-Engineering/terfyn/internal/trace"
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
	err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "pub", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"}, UseExecIR: true})
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
	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "pub", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"}, UseExecIR: true}); !errors.Is(err, ErrInterrupted) {
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
	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "appr", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"}, UseExecIR: true}); !errors.Is(err, ErrInterrupted) {
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
	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "pub", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"}, UseExecIR: true}); !errors.Is(err, ErrInterrupted) {
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
	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "outer", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "hi"}, UseExecIR: true}); !errors.Is(err, ErrInterrupted) {
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

// TestExecIRResume_DagCheckpointRoutesToDag guards backward compatibility: a run
// on the DAG path writes a non-execir checkpoint, so resumeIsExecIR is false and
// resume stays on the DAG path.
func TestExecIRResume_DagCheckpointRoutesToDag(t *testing.T) {
	t.Parallel()
	ex, _, runID, started := newResumeExecutor(t, hitlTestGraph(), "hitl")
	ctx := context.Background()
	// DAG path (UseExecIR unset) suspends at the gate.
	if err := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "hitl", Env: "dev", StartedAt: started, Input: map[string]any{}}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("DAG run should interrupt, got %v", err)
	}
	isExec, err := ex.resumeIsExecIR(ctx, runID)
	if err != nil {
		t.Fatalf("resumeIsExecIR: %v", err)
	}
	if isExec {
		t.Fatalf("a DAG checkpoint must not be flagged execir")
	}
}
