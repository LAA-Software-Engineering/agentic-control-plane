package engine

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
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

func agentLoopGraph(t *testing.T, agent spec.AgentSpec, pol spec.PolicySpec) *spec.ProjectGraph {
	t.Helper()
	if agent.Model == "" {
		agent.Model = "mock/gpt-4"
	}
	if agent.Output == nil {
		agent.Output = &spec.AgentIO{Schema: "./schemas/agent-out.schema.json"}
	}
	return &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Providers: &spec.ProjectProviders{
				Models: map[string]spec.ModelProviderConfig{"mock": {Type: "mock"}},
			},
		},
		Tools: map[string]*spec.ToolResource{
			"helper": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindTool,
				Metadata:   spec.Metadata{Name: "helper"},
				Spec: spec.ToolSpec{
					Type:   "mock",
					Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(false)},
				},
			},
		},
		Agents: map[string]*spec.AgentResource{
			"reviewer": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindAgent,
				Metadata:   spec.Metadata{Name: "reviewer"},
				Spec:       agent,
			},
		},
		Policies: map[string]*spec.PolicyResource{
			"default": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindPolicy,
				Metadata:   spec.Metadata{Name: "default"},
				Spec:       pol,
			},
		},
		Workflows: map[string]*spec.WorkflowResource{
			"demo": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "demo"},
				Spec: spec.WorkflowSpec{
					Policy: "default",
					Steps: []spec.WorkflowStep{
						{ID: "act", Agent: "reviewer", With: map[string]any{"topic": "agents"}},
					},
					Output: &spec.WorkflowOutput{
						Value: map[string]any{"summary": "${steps.act.output.summary}"},
					},
				},
			},
		},
	}
}

func runAgentLoop(t *testing.T, graph *spec.ProjectGraph, mock *models.MockClient, extra tools.ToolExecutor) (*state.Run, []trace.Event, error) {
	t.Helper()
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "agent-loop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	runID := "run-loop"
	started := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "demo", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: `{"topic":"agents"}`,
	}); err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry(graph)
	if extra != nil {
		reg.Mock = extra
	}
	ex := &Executor{
		Graph:        graph,
		ProjectRoot:  testProjectRoot(t),
		Tools:        reg,
		ModelResolve: func(string) (models.ModelClient, string, error) { return mock, "gpt-4", nil },
		Store:        st,
		Trace:        trace.NewRecorder(st),
	}
	runErr := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "demo", Env: "dev", StartedAt: started,
		Input: map[string]any{"topic": "agents"},
	})
	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := trace.NewReader(st).ListByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	return got, events, runErr
}

func TestRun_agentToolLoop_happyPath(t *testing.T) {
	graph := agentLoopGraph(t, spec.AgentSpec{
		Instructions: "Use helper then summarize.",
		Tools:        []string{"helper"},
	}, spec.PolicySpec{})
	mock := &models.MockClient{
		Script: []models.MockTurn{
			{
				ToolCalls: []models.ToolCall{{
					ID:        "call_1",
					Name:      "helper",
					Arguments: json.RawMessage(`{"q":"weather"}`),
				}},
				Meta: &models.GenerateMeta{DurationMs: 2, PromptTokens: 10, CompletionTokens: 4, CostUSD: 0.02},
			},
			{
				Content: `{"summary":"used helper"}`,
				Meta:    &models.GenerateMeta{DurationMs: 3, PromptTokens: 12, CompletionTokens: 6, CostUSD: 0.03},
			},
		},
	}
	got, events, err := runAgentLoop(t, graph, mock, nil)
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
	if out["summary"] != "used helper" {
		t.Fatalf("output %+v", out)
	}
	if mock.CallCount() != 2 {
		t.Fatalf("generates %d", mock.CallCount())
	}
	first := mock.Requests()[0]
	if first.ToolChoice != models.ToolChoiceAuto || len(first.Tools) != 1 || first.Tools[0].Name != "helper" {
		t.Fatalf("first request %+v", first)
	}
	second := mock.Requests()[1]
	if len(second.Messages) < 4 {
		t.Fatalf("messages %+v", second.Messages)
	}
	var sawResult bool
	for _, msg := range second.Messages {
		for _, r := range msg.ToolResults {
			if r.ToolCallID == "call_1" && strings.Contains(r.Content, "tool.helper.default") {
				sawResult = true
			}
		}
	}
	if !sawResult {
		t.Fatalf("missing tool_result in %+v", second.Messages)
	}
	if got.TotalCostUSD < 0.05 {
		t.Fatalf("cost %v, want model+tool loop", got.TotalCostUSD)
	}
	var sawTool, sawLLM int
	for _, ev := range events {
		switch ev.Type {
		case string(trace.EventToolExecution):
			sawTool++
		case string(trace.EventLLMCompletion):
			sawLLM++
		}
	}
	if sawTool == 0 || sawLLM < 2 {
		t.Fatalf("trace tool=%d llm=%d events=%d", sawTool, sawLLM, len(events))
	}
}

func TestRun_agentToolLoop_maxIterations(t *testing.T) {
	graph := agentLoopGraph(t, spec.AgentSpec{
		Tools:       []string{"helper"},
		Constraints: &spec.AgentConstraints{MaxIterations: 2},
	}, spec.PolicySpec{})
	mock := &models.MockClient{
		Script: []models.MockTurn{
			{ToolCalls: []models.ToolCall{{ID: "c1", Name: "helper", Arguments: json.RawMessage(`{}`)}}},
			{ToolCalls: []models.ToolCall{{ID: "c2", Name: "helper", Arguments: json.RawMessage(`{}`)}}},
			{Content: `{"summary":"should not run"}`},
		},
	}
	got, events, err := runAgentLoop(t, graph, mock, nil)
	if err == nil || !strings.Contains(err.Error(), "maxIterations") {
		t.Fatalf("err = %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("status %q", got.Status)
	}
	if mock.CallCount() != 2 {
		t.Fatalf("generates %d, want 2", mock.CallCount())
	}
	var sawLimit bool
	for _, ev := range events {
		if ev.Type == string(trace.EventLimitHit) && strings.Contains(ev.DataJSON, "max_iterations") {
			sawLimit = true
		}
	}
	if !sawLimit {
		t.Fatalf("expected limit_hit max_iterations, events=%+v", events)
	}
}

func TestRun_agentToolLoop_policyDenied(t *testing.T) {
	graph := agentLoopGraph(t, spec.AgentSpec{Tools: []string{"helper"}}, spec.PolicySpec{
		Approvals: &spec.PolicyApprovals{RequiredFor: []string{"tool.helper.default"}},
	})
	mock := &models.MockClient{
		Script: []models.MockTurn{
			{ToolCalls: []models.ToolCall{{ID: "c1", Name: "helper", Arguments: json.RawMessage(`{}`)}}},
			{Content: `{"summary":"nope"}`},
		},
	}
	got, events, err := runAgentLoop(t, graph, mock, nil)
	if err == nil {
		t.Fatal("expected policy denial")
	}
	d, ok := policy.AsDenied(err)
	if !ok || d.Reason != policy.ReasonApprovalRequired {
		t.Fatalf("denied %+v err=%v", d, err)
	}
	if got.Status != "failed" {
		t.Fatalf("status %q", got.Status)
	}
	if mock.CallCount() != 1 {
		t.Fatalf("generates %d, want 1 (denied before second turn)", mock.CallCount())
	}
	var sawDeny bool
	for _, ev := range events {
		if ev.Type == string(trace.EventSystemError) && strings.Contains(ev.DataJSON, policy.ReasonApprovalRequired) {
			sawDeny = true
		}
	}
	if !sawDeny {
		t.Fatalf("expected system_error approval_required, events=%+v", events)
	}
}

func TestRun_agentToolLoop_noToolsRegression(t *testing.T) {
	graph := agentLoopGraph(t, spec.AgentSpec{Instructions: "Just answer."}, spec.PolicySpec{})
	mock := &models.MockClient{Content: `{"summary":"plain"}`}
	got, _, err := runAgentLoop(t, graph, mock, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "succeeded" {
		t.Fatalf("status %q err=%q", got.Status, got.ErrorText)
	}
	if mock.CallCount() != 1 {
		t.Fatalf("generates %d", mock.CallCount())
	}
	req := mock.LastRequest()
	if len(req.Tools) != 0 || req.ToolChoice != "" {
		t.Fatalf("no-tools request should omit tools: %+v", req)
	}
	if req.Model == "" || len(req.Messages) != 2 {
		t.Fatalf("request %+v", req)
	}
}

func TestRun_agentToolLoop_undeclaredTool(t *testing.T) {
	graph := agentLoopGraph(t, spec.AgentSpec{Tools: []string{"helper"}}, spec.PolicySpec{})
	mock := &models.MockClient{
		Script: []models.MockTurn{
			{ToolCalls: []models.ToolCall{{ID: "c1", Name: "ghost", Arguments: json.RawMessage(`{}`)}}},
		},
	}
	_, _, err := runAgentLoop(t, graph, mock, nil)
	if err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_agentToolLoop_rejectsUnadvertisedOperation(t *testing.T) {
	names := []string{"helper.echo", "tool.helper.echo", "helper.command.run", "helper.delete.users"}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			graph := agentLoopGraph(t, spec.AgentSpec{Tools: []string{"helper"}}, spec.PolicySpec{})
			mock := &models.MockClient{
				Script: []models.MockTurn{
					{ToolCalls: []models.ToolCall{{ID: "c1", Name: name, Arguments: json.RawMessage(`{}`)}}},
				},
			}
			calls := 0
			extra := &tools.MockExecutor{Fn: func(ctx context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
				calls++
				return tools.ToolCallResponse{Output: map[string]any{"used": req.Uses}}, nil
			}}
			_, _, err := runAgentLoop(t, graph, mock, extra)
			if err == nil || !strings.Contains(err.Error(), "not declared") {
				t.Fatalf("err = %v", err)
			}
			if calls != 0 {
				t.Fatalf("Tools.Call ran %d times; unadvertised ops must fail before Call", calls)
			}
		})
	}
}

func TestRun_agentToolLoop_maxIterationsDoesNotExecuteLastToolUse(t *testing.T) {
	graph := agentLoopGraph(t, spec.AgentSpec{
		Tools:       []string{"helper"},
		Constraints: &spec.AgentConstraints{MaxIterations: 1},
	}, spec.PolicySpec{})
	mock := &models.MockClient{
		Script: []models.MockTurn{
			{ToolCalls: []models.ToolCall{{ID: "c1", Name: "helper", Arguments: json.RawMessage(`{}`)}}},
		},
	}
	calls := 0
	extra := &tools.MockExecutor{Fn: func(ctx context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
		calls++
		return tools.ToolCallResponse{Output: map[string]any{"used": req.Uses}}, nil
	}}
	got, events, err := runAgentLoop(t, graph, mock, extra)
	if err == nil || !strings.Contains(err.Error(), "maxIterations") {
		t.Fatalf("err = %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("status %q", got.Status)
	}
	if mock.CallCount() != 1 {
		t.Fatalf("generates %d, want 1", mock.CallCount())
	}
	if calls != 0 {
		t.Fatalf("Tools.Call ran %d times; last-turn tool_use must not execute", calls)
	}
	var sawLimit bool
	for _, ev := range events {
		if ev.Type == string(trace.EventLimitHit) && strings.Contains(ev.DataJSON, "max_iterations") {
			sawLimit = true
		}
	}
	if !sawLimit {
		t.Fatalf("expected limit_hit max_iterations, events=%+v", events)
	}
}
