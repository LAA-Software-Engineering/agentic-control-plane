package engine

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
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
		Policies: map[string]*spec.PolicyResource{
			"default": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindPolicy,
				Metadata:   spec.Metadata{Name: "default"},
				Spec:       spec.PolicySpec{},
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
