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

func demoWorkflowGraph(t *testing.T) *spec.ProjectGraph {
	t.Helper()
	return &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Providers: &spec.ProjectProviders{
				Models: map[string]spec.ModelProviderConfig{
					"mock": {Type: "mock"},
				},
			},
		},
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
		Agents: map[string]*spec.AgentResource{
			"reviewer": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindAgent,
				Metadata:   spec.Metadata{Name: "reviewer"},
				Spec: spec.AgentSpec{
					Model:        "mock/gpt-4",
					Instructions: "Summarize the tool payload as JSON.",
					Output:       &spec.AgentIO{Schema: "./schemas/agent-out.schema.json"},
				},
			},
		},
		Workflows: map[string]*spec.WorkflowResource{
			"demo": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "demo"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{
						{
							ID:   "fetch",
							Uses: "tool.helper.echo",
							With: map[string]any{"topic": "${input.topic}"},
						},
						{
							ID:    "summarize",
							Agent: "reviewer",
							With:  map[string]any{"echo": "${steps.fetch.output.echo}"},
						},
					},
					Output: &spec.WorkflowOutput{
						Value: map[string]any{
							"topic":   "${input.topic}",
							"summary": "${steps.summarize.output.summary}",
						},
					},
				},
			},
		},
	}
}

func TestRun_interruptAndResume_completesWithoutReplay(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "resume.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := testProjectRoot(t)
	graph := demoWorkflowGraph(t)
	runID := "run-resume"
	started := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	inJSON := `{"topic":"agents"}`
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "demo", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: inJSON, TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(inJSON), &input); err != nil {
		t.Fatal(err)
	}

	ex := &Executor{
		Graph: graph, ProjectRoot: root,
		Tools: tools.NewRegistry(graph), Models: models.NewRegistry(graph),
		Store: st, Trace: trace.NewRecorder(st),
		Now: func() time.Time { return started },
	}
	err = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "demo", Env: "dev", StartedAt: started, Input: input,
		InterruptAfterStepIndex: intPtr(0),
	})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("err = %v want ErrInterrupted", err)
	}

	run, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != state.RunStatusInterrupted {
		t.Fatalf("status = %q", run.Status)
	}
	cp, err := st.GetLatestCheckpoint(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if cp.Status != state.CheckpointStatusInterrupted || cp.StepIndex != 0 || cp.StepID != "fetch" {
		t.Fatalf("checkpoint = %+v", cp)
	}

	if err := st.UpdateRunStatus(ctx, runID, "running"); err != nil {
		t.Fatal(err)
	}
	resumeAt := started.Add(time.Hour)
	ex.Now = func() time.Time { return resumeAt }
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "demo", Env: "dev", StartedAt: started, Input: input,
		Resume: true,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "succeeded" {
		t.Fatalf("status %q err=%q", got.Status, got.ErrorText)
	}

	rows, err := trace.NewReader(st).ListByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	var fetchSelections int
	for _, ev := range rows {
		if ev.StepID == "fetch" && ev.Type == string(trace.EventToolSelection) {
			fetchSelections++
		}
	}
	if fetchSelections != 1 {
		t.Fatalf("fetch tool_selection count = %d want 1", fetchSelections)
	}
}

func TestRun_resumeFromRunningCheckpoint(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "crash.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := testProjectRoot(t)
	graph := demoWorkflowGraph(t)
	runID := "run-crash"
	started := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	inJSON := `{"topic":"crash-test"}`
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "demo", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: inJSON, TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(inJSON), &input); err != nil {
		t.Fatal(err)
	}

	ex := &Executor{
		Graph: graph, ProjectRoot: root,
		Tools: tools.NewRegistry(graph), Models: models.NewRegistry(graph),
		Store: st, Trace: trace.NewRecorder(st),
		Now: func() time.Time { return started },
	}
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "demo", Env: "dev", StartedAt: started, Input: input,
		InterruptAfterStepIndex: intPtr(0),
	}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("interrupt: %v", err)
	}
	if err := st.UpdateRunStatus(ctx, runID, "running"); err != nil {
		t.Fatal(err)
	}

	cp, err := st.GetLatestCheckpoint(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveCheckpoint(ctx, state.RunCheckpoint{
		RunID: runID, StepIndex: cp.StepIndex, StepID: cp.StepID,
		ContextJSON: cp.ContextJSON, Status: state.CheckpointStatusRunning,
		CreatedAt: started.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "demo", Env: "dev", StartedAt: started, Input: input,
		Resume: true,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "succeeded" {
		t.Fatalf("status %q", got.Status)
	}
}

func TestRun_resume_rejectsCompletedCheckpoint(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "done.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	started := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: "done", WorkflowName: "demo", Env: "dev", Status: "succeeded",
		StartedAt: started, InputJSON: `{}`, TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveCheckpoint(ctx, state.RunCheckpoint{
		RunID: "done", StepIndex: 1, StepID: "last",
		ContextJSON: `{"version":1,"input":{},"steps":{},"totalCostUsd":0}`,
		Status:      state.CheckpointStatusCompleted, CreatedAt: started,
	}); err != nil {
		t.Fatal(err)
	}

	ex := &Executor{Graph: demoWorkflowGraph(t), Store: st}
	err = ex.Run(ctx, RunInput{RunID: "done", WorkflowName: "demo", Resume: true, Input: map[string]any{}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func intPtr(n int) *int { return &n }
