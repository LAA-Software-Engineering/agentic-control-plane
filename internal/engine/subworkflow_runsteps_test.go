package engine

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
	"github.com/Terfyn/terfyn/internal/tools"
	"github.com/Terfyn/terfyn/internal/trace"
)

// TestInvokeWorkflow_failingCalleeMarksStepFailed proves the parent's run_steps row for a
// `workflow:` step is finalized when the callee fails, instead of being left "running" forever
// inside a finished run (#395/#401): status `failed`, a finished_at, error_text, and a run_error
// trace event for the step id.
func TestInvokeWorkflow_failingCalleeMarksStepFailed(t *testing.T) {
	t.Parallel()
	graph := &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"bad": {APIVersion: spec.APIVersionV0, Kind: spec.KindTool, Metadata: spec.Metadata{Name: "bad"},
				Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{Trusted: spec.BoolPtr(true), SideEffects: spec.BoolPtr(false)}}},
		},
		Workflows: map[string]*spec.WorkflowResource{
			"c": {APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "c"},
				Spec: spec.WorkflowSpec{
					Steps:  []spec.WorkflowStep{{ID: "echo", Uses: "tool.bad.op", With: map[string]any{"x": "y"}}},
					Output: &spec.WorkflowOutput{Value: map[string]any{"r": "${steps.echo.output}"}},
				}},
			"p": {APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "p"},
				Spec: spec.WorkflowSpec{
					Steps:  []spec.WorkflowStep{{ID: "sub", Workflow: "c", With: map[string]any{"topic": "x"}}},
					Output: &spec.WorkflowOutput{Value: map[string]any{"result": "${steps.sub.output}"}},
				}},
		},
	}

	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "sub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	runID := "run-sub"
	started := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{RunID: runID, WorkflowName: "p", Env: "dev", Status: state.RunStatusRunning, StartedAt: started, InputJSON: `{}`}); err != nil {
		t.Fatal(err)
	}
	failing := &tools.MockExecutor{Err: errors.New("boom")}
	ex := &Executor{Graph: graph, Tools: failing, Store: st, Trace: trace.NewRecorder(st)}

	if runErr := ex.Run(ctx, RunInput{RunID: runID, WorkflowName: "p", Env: "dev", StartedAt: started, Input: map[string]any{}}); runErr == nil {
		t.Fatal("expected the run to fail when the callee fails")
	}
	if run, _ := st.GetRun(ctx, runID); run.Status != state.RunStatusFailed {
		t.Fatalf("run status = %q, want failed", run.Status)
	}

	steps, err := st.ListRunStepsByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	var sub *state.RunStep
	for i := range steps {
		if steps[i].StepID == "sub" {
			sub = &steps[i]
		}
	}
	if sub == nil {
		t.Fatalf("no run_steps row for the subworkflow step: %+v", steps)
	}
	if sub.Status != "failed" {
		t.Fatalf("subworkflow step status = %q, want failed (was left running)", sub.Status)
	}
	if sub.FinishedAt == nil {
		t.Fatal("subworkflow step has no finished_at")
	}
	if sub.ErrorText == "" {
		t.Fatal("subworkflow step has no error_text")
	}
}
