package engine

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
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
