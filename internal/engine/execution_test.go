package engine

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

func testProjectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "testdata", "wfproj"))
}

func TestRun_sequentialToolAndAgent_mockModel(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "run.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := testProjectRoot(t)
	graph := &spec.ProjectGraph{
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
							With: map[string]any{
								"topic": "${input.topic}",
								"extra": "x",
							},
						},
						{
							ID:    "summarize",
							Agent: "reviewer",
							With: map[string]any{
								"echo": "${steps.fetch.output.echo}",
							},
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

	runID := "run-1"
	started := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	inJSON := `{"topic":"agents"}`
	if err := st.StartRun(ctx, state.Run{
		RunID:        runID,
		WorkflowName: "demo",
		Env:          "dev",
		Status:       "running",
		StartedAt:    started,
		InputJSON:    inJSON,
		TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}

	var input map[string]any
	if err := json.Unmarshal([]byte(inJSON), &input); err != nil {
		t.Fatal(err)
	}

	ex := &Executor{
		Graph:       graph,
		ProjectRoot: root,
		Tools:       tools.NewRegistry(graph),
		Models:      models.NewRegistry(graph),
		Store:       st,
		Trace:       trace.NewRecorder(st),
	}
	if err := ex.Run(ctx, RunInput{
		RunID:        runID,
		WorkflowName: "demo",
		Env:          "dev",
		StartedAt:    started,
		Input:        input,
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
	var out map[string]any
	if err := json.Unmarshal([]byte(got.OutputJSON), &out); err != nil {
		t.Fatal(err)
	}
	if out["topic"] != "agents" {
		t.Fatalf("topic %+v", out)
	}
	if out["summary"] != "mock" {
		t.Fatalf("summary %+v", out)
	}

	events, err := trace.NewReader(st).ListByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 {
		t.Fatalf("expected trace events, got %d", len(events))
	}
}

func TestRun_agentOutputSchemaInvalid_failsRun(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "run2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := testProjectRoot(t)
	graph := &spec.ProjectGraph{
		Policies: map[string]*spec.PolicyResource{
			"default": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindPolicy,
				Metadata:   spec.Metadata{Name: "default"},
				Spec:       spec.PolicySpec{},
			},
		},
		Agents: map[string]*spec.AgentResource{
			"bad": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindAgent,
				Metadata:   spec.Metadata{Name: "bad"},
				Spec: spec.AgentSpec{
					Model:        "mock/x",
					Instructions: "Return JSON.",
					Output:       &spec.AgentIO{Schema: "./schemas/agent-out.schema.json"},
				},
			},
		},
		Workflows: map[string]*spec.WorkflowResource{
			"one": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "one"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{
						{ID: "only", Agent: "bad", With: map[string]any{}},
					},
				},
			},
		},
	}

	runID := "run-bad"
	started := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID:        runID,
		WorkflowName: "one",
		Env:          "dev",
		Status:       "running",
		StartedAt:    started,
		InputJSON:    `{}`,
		TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}

	ex := &Executor{
		Graph:       graph,
		ProjectRoot: root,
		Tools:       tools.NewRegistry(graph),
		ModelResolve: func(modelRef string) (models.ModelClient, string, error) {
			_ = modelRef
			return &models.MockClient{Content: `{"findings":[]}`}, "x", nil
		},
		Store: st,
	}
	err = ex.Run(ctx, RunInput{
		RunID:        runID,
		WorkflowName: "one",
		Env:          "dev",
		StartedAt:    started,
		Input:        map[string]any{},
	})
	if err == nil {
		t.Fatal("expected schema validation error")
	}

	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" {
		t.Fatalf("status %q", got.Status)
	}
}

func TestWithAgentRetry_transientTwiceSucceeds(t *testing.T) {
	ctx := context.Background()
	var n int
	err := withAgentRetry(ctx, func() error {
		n++
		if n == 1 {
			return ErrTransientGeneration
		}
		return nil
	})
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
