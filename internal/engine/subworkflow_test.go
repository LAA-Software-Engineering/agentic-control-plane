package engine

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

// subworkflowGraph is outer -> (workflow: inner) -> tool.helper.echo.
func subworkflowGraph() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"helper": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindTool,
				Metadata:   spec.Metadata{Name: "helper"},
				Spec: spec.ToolSpec{
					Type:   "native",
					Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(false)},
				},
			},
		},
		Policies: map[string]*spec.PolicyResource{
			"default": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindPolicy,
				Metadata:   spec.Metadata{Name: "default"},
				Spec:       spec.PolicySpec{},
			},
		},
		Workflows: map[string]*spec.WorkflowResource{
			"inner": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "inner"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{
						{ID: "e", Uses: "tool.helper.echo", With: map[string]any{"msg": "${input.msg}"}},
					},
					Output: &spec.WorkflowOutput{
						Value: map[string]any{"echoed": "${steps.e.output.echo.msg}"},
					},
				},
			},
			"outer": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "outer"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{
						{ID: "call", Workflow: "inner", With: map[string]any{"msg": "${input.topic}"}},
					},
					Output: &spec.WorkflowOutput{
						Value: map[string]any{"result": "${steps.call.output.echoed}"},
					},
				},
			},
		},
	}
}

func newSubworkflowExecutor(t *testing.T, st *sqlite.Store, graph *spec.ProjectGraph, clock func() time.Time) *Executor {
	t.Helper()
	return &Executor{
		Graph:       graph,
		ProjectRoot: testProjectRoot(t),
		Tools:       tools.NewRegistry(graph),
		Models:      models.NewRegistry(graph),
		Store:       st,
		Trace:       trace.NewRecorder(st),
		Now:         clock,
	}
}

func TestRun_subworkflow_nestedExecutionAndOutput(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "sub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := subworkflowGraph()
	started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	runID := "outer-run"
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "outer", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: `{"topic":"hello"}`,
	}); err != nil {
		t.Fatal(err)
	}

	ex := newSubworkflowExecutor(t, st, graph, func() time.Time { return started })
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "outer", Env: "dev", StartedAt: started,
		Input: map[string]any{"topic": "hello"},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.RunStatusSucceeded {
		t.Fatalf("status %q err=%q", got.Status, got.ErrorText)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(got.OutputJSON), &out); err != nil {
		t.Fatal(err)
	}
	if out["result"] != "hello" {
		t.Fatalf("result = %+v", out)
	}

	// The subworkflow ran as a child run linked to the parent.
	childID := subworkflowRunID(runID, "call")
	child, err := st.GetRun(ctx, childID)
	if err != nil {
		t.Fatalf("child run missing: %v", err)
	}
	if child.ParentRunID != runID {
		t.Fatalf("child ParentRunID = %q want %q", child.ParentRunID, runID)
	}
	if child.WorkflowName != "inner" || child.Status != state.RunStatusSucceeded {
		t.Fatalf("child = %+v", child)
	}
	// Child trace is self-contained (nested run_started + the tool step events).
	events, err := trace.NewReader(st).ListByRunID(ctx, childID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("expected nested child trace events")
	}
}

func TestRun_subworkflow_resumeMidSubworkflow(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "sub-resume.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := subworkflowGraph()
	started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	runID := "outer-resume"
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "outer", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: `{"topic":"hello"}`,
	}); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"topic": "hello"}

	ex := newSubworkflowExecutor(t, st, graph, func() time.Time { return started })
	// Interrupt inside the subworkflow after its step "e" completes.
	err = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "outer", Env: "dev", StartedAt: started,
		Input: input, InterruptAfterStepID: "e",
	})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("err = %v want ErrInterrupted", err)
	}

	parent, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != state.RunStatusInterrupted {
		t.Fatalf("parent status = %q", parent.Status)
	}
	childID := subworkflowRunID(runID, "call")
	child, err := st.GetRun(ctx, childID)
	if err != nil {
		t.Fatalf("child run missing after interrupt: %v", err)
	}
	if child.Status != state.RunStatusInterrupted {
		t.Fatalf("child status = %q want interrupted", child.Status)
	}

	// Resume the parent: the child resumes from its checkpoint and both complete.
	if err := st.UpdateRunStatus(ctx, runID, "running"); err != nil {
		t.Fatal(err)
	}
	resumeAt := started.Add(time.Hour)
	ex.Now = func() time.Time { return resumeAt }
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "outer", Env: "dev", StartedAt: started,
		Input: input, Resume: true,
	}); err != nil {
		t.Fatalf("resume: %v", err)
	}

	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.RunStatusSucceeded {
		t.Fatalf("status %q err=%q", got.Status, got.ErrorText)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(got.OutputJSON), &out); err != nil {
		t.Fatal(err)
	}
	if out["result"] != "hello" {
		t.Fatalf("result = %+v", out)
	}

	// The inner tool step ran exactly once across interrupt + resume (no replay).
	childEvents, err := trace.NewReader(st).ListByRunID(ctx, childID)
	if err != nil {
		t.Fatal(err)
	}
	var selections int
	for _, ev := range childEvents {
		if ev.StepID == "e" && ev.Type == string(trace.EventToolSelection) {
			selections++
		}
	}
	if selections != 1 {
		t.Fatalf("inner tool_selection count = %d want 1", selections)
	}
}

// nestedHitlGraph nests a gated tool call one level deep: outer -> (workflow: inner) ->
// tool.helper.echo, where inner's policy requires approval + HITL interrupt for the tool.
func nestedHitlGraph() *spec.ProjectGraph {
	g := hitlTestGraph()
	g.Policies["gate"].Metadata = spec.Metadata{Name: "gate"}
	// Rename the gated workflow to "inner" and add an "outer" that invokes it.
	inner := g.Workflows["hitl"]
	inner.Metadata = spec.Metadata{Name: "inner"}
	delete(g.Workflows, "hitl")
	g.Workflows["inner"] = inner
	g.Workflows["outer"] = &spec.WorkflowResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindWorkflow,
		Metadata:   spec.Metadata{Name: "outer"},
		Spec: spec.WorkflowSpec{
			Policy: "gate",
			Steps: []spec.WorkflowStep{
				{ID: "call", Workflow: "inner", With: map[string]any{}},
			},
			Output: &spec.WorkflowOutput{Value: map[string]any{"result": "${steps.call.output.echo}"}},
		},
	}
	return g
}

func TestRun_subworkflow_nestedHitlResolvedThroughParent(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "sub-hitl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := nestedHitlGraph()
	started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	runID := "outer-hitl"
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "outer", Env: "local", Status: "running",
		StartedAt: started, InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	ex := newSubworkflowExecutor(t, st, graph, func() time.Time { return started })

	// First run: the gated tool inside the callee interrupts for approval.
	err = ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "outer", Env: "local", StartedAt: started, Input: map[string]any{}})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("first run: %v want ErrInterrupted", err)
	}
	parent, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != state.RunStatusInterrupted {
		t.Fatalf("parent status = %q", parent.Status)
	}

	// The invariant: the run the operator started carries the nested gate on its own checkpoint.
	cp, err := st.GetLatestCheckpoint(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	var payload checkpointPayload
	if err := json.Unmarshal([]byte(cp.ContextJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PendingHitl == nil {
		t.Fatal("parent checkpoint must carry the lifted pending gate")
	}
	if payload.PendingHitl.Uses != "tool.helper.echo" {
		t.Fatalf("gate uses = %q", payload.PendingHitl.Uses)
	}
	// The gate is visible in the parent's own trace.
	parentEvents, err := trace.NewReader(st).ListByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	var reqOnParent bool
	for _, ev := range parentEvents {
		if ev.Type == string(trace.EventHitlRequestCreated) {
			reqOnParent = true
		}
	}
	if !reqOnParent {
		t.Fatal("parent trace must show the nested hitl_request_created")
	}

	// Resume the parent with an approval decision; the callee resolves and both complete.
	if err := st.UpdateRunStatus(ctx, runID, "running"); err != nil {
		t.Fatal(err)
	}
	err = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "outer", Env: "local", StartedAt: started, Input: map[string]any{},
		Resume: true,
		Hitl: HitlRunOptions{
			Actor:    "alice",
			Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"},
		},
	})
	if err != nil {
		t.Fatalf("resume with decision: %v", err)
	}
	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.RunStatusSucceeded {
		t.Fatalf("status %q err=%q", got.Status, got.ErrorText)
	}
	// The lifted gate is cleared once the callee completes.
	finalCp, err := st.GetLatestCheckpoint(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	var finalPayload checkpointPayload
	if err := json.Unmarshal([]byte(finalCp.ContextJSON), &finalPayload); err != nil {
		t.Fatal(err)
	}
	if finalPayload.PendingHitl != nil {
		t.Fatalf("lifted gate must clear after resolution: %+v", finalPayload.PendingHitl)
	}
}

func TestRun_subworkflow_runningChildNoCheckpoint_reRunsFresh(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "sub-crash.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := subworkflowGraph()
	started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	runID := "outer-crash"
	input := map[string]any{"topic": "hello"}
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "outer", Env: "dev", Status: state.RunStatusInterrupted,
		StartedAt: started, InputJSON: `{"topic":"hello"}`,
	}); err != nil {
		t.Fatal(err)
	}
	// Parent interrupted at the subworkflow step, which never completed.
	if err := st.SaveCheckpoint(ctx, state.RunCheckpoint{
		RunID: runID, StepIndex: 0, StepID: "call",
		ContextJSON: `{"version":1,"input":{"topic":"hello"},"steps":{},"totalCostUsd":0}`,
		Status:      state.CheckpointStatusInterrupted, CreatedAt: started,
	}); err != nil {
		t.Fatal(err)
	}
	// Child row exists as Running with NO checkpoint (crash between StartRun and first save).
	childID := subworkflowRunID(runID, "call")
	if err := st.StartRun(ctx, state.Run{
		RunID: childID, WorkflowName: "inner", Env: "dev", Status: state.RunStatusRunning,
		StartedAt: started, InputJSON: `{"msg":"hello"}`, ParentRunID: runID,
	}); err != nil {
		t.Fatal(err)
	}

	ex := newSubworkflowExecutor(t, st, graph, func() time.Time { return started })
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "outer", Env: "dev", StartedAt: started, Input: input, Resume: true,
	}); err != nil {
		t.Fatalf("resume: %v", err)
	}

	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.RunStatusSucceeded {
		t.Fatalf("parent status %q err=%q", got.Status, got.ErrorText)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(got.OutputJSON), &out); err != nil {
		t.Fatal(err)
	}
	if out["result"] != "hello" {
		t.Fatalf("result = %+v", out)
	}
	child, err := st.GetRun(ctx, childID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != state.RunStatusSucceeded {
		t.Fatalf("child status = %q", child.Status)
	}
}

func TestRun_subworkflow_missingCalleeFails(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "sub-missing.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := subworkflowGraph()
	graph.Workflows["outer"].Spec.Steps[0].Workflow = "ghost"
	started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	runID := "outer-missing"
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "outer", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: `{"topic":"hello"}`,
	}); err != nil {
		t.Fatal(err)
	}

	ex := newSubworkflowExecutor(t, st, graph, func() time.Time { return started })
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "outer", Env: "dev", StartedAt: started,
		Input: map[string]any{"topic": "hello"},
	}); err == nil {
		t.Fatal("expected failure for missing callee")
	}
	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.RunStatusFailed {
		t.Fatalf("status = %q", got.Status)
	}
}
