package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

func helperTool() *spec.ToolResource {
	return &spec.ToolResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindTool,
		Metadata:   spec.Metadata{Name: "helper"},
		Spec: spec.ToolSpec{
			Type:   "native",
			Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(false)},
		},
	}
}

func defaultPolicy() *spec.PolicyResource {
	return &spec.PolicyResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindPolicy,
		Metadata:   spec.Metadata{Name: "default"},
		Spec:       spec.PolicySpec{},
	}
}

func subworkflowGraph() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Providers: &spec.ProjectProviders{
				Models: map[string]spec.ModelProviderConfig{"mock": {Type: "mock"}},
			},
		},
		Tools:    map[string]*spec.ToolResource{"helper": helperTool()},
		Policies: map[string]*spec.PolicyResource{"default": defaultPolicy()},
		Workflows: map[string]*spec.WorkflowResource{
			"child": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "child"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{
						{
							ID:   "echo",
							Uses: "tool.helper.echo",
							With: map[string]any{"msg": "${input.msg}"},
						},
					},
					Output: &spec.WorkflowOutput{
						Value: map[string]any{
							"greeting": "${steps.echo.output.echo.msg}",
						},
					},
				},
			},
			"parent": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "parent"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{
						{
							ID:       "call",
							Workflow: "child",
							With:     map[string]any{"msg": "${input.topic}"},
						},
						{
							ID:   "after",
							Uses: "tool.helper.echo",
							With: map[string]any{"from_child": "${steps.call.output.greeting}"},
						},
					},
					Output: &spec.WorkflowOutput{
						Value: map[string]any{
							"final": "${steps.after.output.echo.from_child}",
						},
					},
				},
			},
		},
	}
}

func TestRun_subworkflowConsumesCalleeOutput(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "sub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := subworkflowGraph()
	runID := "run-sub"
	started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	inJSON := `{"topic":"hello"}`
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "parent", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: inJSON,
	}); err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(inJSON), &input); err != nil {
		t.Fatal(err)
	}

	ex := &Executor{
		Graph: graph, ProjectRoot: t.TempDir(),
		Tools: tools.NewRegistry(graph), Models: models.NewRegistry(graph),
		Store: st, Trace: trace.NewRecorder(st),
	}
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "parent", Env: "dev", StartedAt: started, Input: input,
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
	if out["final"] != "hello" {
		t.Fatalf("final %+v", out)
	}

	events, err := trace.NewReader(st).ListByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	var startedCall, finishedCall bool
	var nestedSelection bool
	for _, ev := range events {
		switch ev.Type {
		case string(trace.EventWorkflowCallStarted):
			startedCall = true
			if ev.StepID != "call" {
				t.Fatalf("call started stepId %q", ev.StepID)
			}
			if !strings.Contains(ev.DataJSON, `"workflow":"child"`) {
				t.Fatalf("call started data %s", ev.DataJSON)
			}
		case string(trace.EventWorkflowCallFinished):
			finishedCall = true
		case string(trace.EventToolSelection):
			if ev.StepID == "call/echo" {
				nestedSelection = true
			}
		}
	}
	if !startedCall || !finishedCall {
		t.Fatalf("missing workflow_call events in %d traces", len(events))
	}
	if !nestedSelection {
		t.Fatalf("expected nested tool_selection, events=%d", len(events))
	}
	var sawCallStack bool
	for _, ev := range events {
		if strings.Contains(ev.DataJSON, `"callStack"`) && strings.Contains(ev.DataJSON, "child") {
			sawCallStack = true
			break
		}
	}
	if !sawCallStack {
		t.Fatal("expected callStack on nested trace events")
	}

	steps, err := st.ListRunStepsByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, s := range steps {
		ids[s.StepID] = s.Status
	}
	if ids["call"] != "succeeded" || ids["after"] != "succeeded" {
		t.Fatalf("outer steps %+v", ids)
	}
	if ids["call/echo"] != "succeeded" {
		t.Fatalf("inner step must be qualified call/echo, got %+v", ids)
	}
	if _, ok := ids["echo"]; ok {
		t.Fatalf("unqualified inner id echo must not be persisted: %+v", ids)
	}
}

func resumeNestedGraph() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Providers: &spec.ProjectProviders{
				Models: map[string]spec.ModelProviderConfig{"mock": {Type: "mock"}},
			},
		},
		Tools:    map[string]*spec.ToolResource{"helper": helperTool()},
		Policies: map[string]*spec.PolicyResource{"default": defaultPolicy()},
		Workflows: map[string]*spec.WorkflowResource{
			"child": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "child"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{
						{ID: "first", Uses: "tool.helper.echo", With: map[string]any{"n": 1}},
						{ID: "second", Uses: "tool.helper.echo", With: map[string]any{"n": 2, "prev": "${steps.first.output.echo.n}"}},
					},
					Output: &spec.WorkflowOutput{
						Value: map[string]any{"n": "${steps.second.output.echo.n}"},
					},
				},
			},
			"parent": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "parent"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{
						{ID: "call", Workflow: "child", With: map[string]any{}},
						{ID: "after", Uses: "tool.helper.echo", With: map[string]any{"done": "${steps.call.output.n}"}},
					},
					Output: &spec.WorkflowOutput{
						Value: map[string]any{"done": "${steps.after.output.echo.done}"},
					},
				},
			},
		},
	}
}

func TestRun_resumeMidSubworkflow(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "resume-sub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := resumeNestedGraph()
	runID := "run-resume-sub"
	started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	inJSON := `{}`
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "parent", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: inJSON,
	}); err != nil {
		t.Fatal(err)
	}

	ex := &Executor{
		Graph: graph, ProjectRoot: t.TempDir(),
		Tools: tools.NewRegistry(graph), Models: models.NewRegistry(graph),
		Store: st, Trace: trace.NewRecorder(st),
		Now: func() time.Time { return started },
	}
	err = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "parent", Env: "dev", StartedAt: started, Input: map[string]any{},
		InterruptAfterStepID: "call/first",
	})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("err = %v want ErrInterrupted", err)
	}

	cp, err := st.GetLatestCheckpoint(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if cp.Status != state.CheckpointStatusInterrupted {
		t.Fatalf("checkpoint status %q", cp.Status)
	}
	if !strings.Contains(cp.ContextJSON, `"workflow":"child"`) || !strings.Contains(cp.ContextJSON, `"stepId":"call"`) {
		t.Fatalf("nested checkpoint missing: %s", cp.ContextJSON)
	}
	if !strings.Contains(cp.ContextJSON, `"first"`) {
		t.Fatalf("inner first missing from nested checkpoint: %s", cp.ContextJSON)
	}

	if err := st.UpdateRunStatus(ctx, runID, state.RunStatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "parent", Env: "dev", StartedAt: started, Input: map[string]any{},
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
	var out map[string]any
	if err := json.Unmarshal([]byte(got.OutputJSON), &out); err != nil {
		t.Fatal(err)
	}
	if out["done"] != float64(2) {
		t.Fatalf("done %+v", out)
	}

	rows, err := trace.NewReader(st).ListByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	var firstSelections int
	for _, ev := range rows {
		if ev.Type != string(trace.EventToolSelection) {
			continue
		}
		if ev.StepID == "call/first" {
			firstSelections++
		}
	}
	if firstSelections != 1 {
		t.Fatalf("first step replayed: %d tool_selection events", firstSelections)
	}
}

func TestRun_subworkflowNestingDepthRuntime(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "depth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Providers: &spec.ProjectProviders{
				Models: map[string]spec.ModelProviderConfig{"mock": {Type: "mock"}},
			},
		},
		Tools:    map[string]*spec.ToolResource{"helper": helperTool()},
		Policies: map[string]*spec.PolicyResource{"default": defaultPolicy()},
		Workflows: map[string]*spec.WorkflowResource{
			"leaf": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "leaf"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{{ID: "n", Uses: "tool.helper.echo", With: map[string]any{"x": 1}}},
				},
			},
			"child": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "child"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{{ID: "down", Workflow: "leaf"}},
				},
			},
			"parent": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "parent"},
				Spec: spec.WorkflowSpec{
					Limits: &spec.ExecutionLimits{MaxWorkflowNesting: 1},
					Steps:  []spec.WorkflowStep{{ID: "call", Workflow: "child"}},
				},
			},
		},
	}

	runID := "run-depth"
	started := time.Now().UTC()
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "parent", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}

	ex := &Executor{
		Graph: graph, ProjectRoot: t.TempDir(),
		Tools: tools.NewRegistry(graph), Models: models.NewRegistry(graph),
		Store: st, Trace: trace.NewRecorder(st),
	}
	err = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "parent", Env: "dev", StartedAt: started, Input: map[string]any{},
	})
	if err == nil || !strings.Contains(err.Error(), "maxWorkflowNesting") {
		t.Fatalf("got %v", err)
	}
}

func TestQualifyStepID_rejectsSlash(t *testing.T) {
	if _, err := qualifyStepID("", "nest/inner"); err == nil || !strings.Contains(err.Error(), "must not contain '/'") {
		t.Fatalf("got %v", err)
	}
	got, err := qualifyStepID("call", "echo")
	if err != nil {
		t.Fatal(err)
	}
	if got != "call/echo" {
		t.Fatalf("got %q", got)
	}
	got, err = qualifyStepID("a/b", "c")
	if err != nil {
		t.Fatal(err)
	}
	if got != "a/b/c" {
		t.Fatalf("got %q", got)
	}
}

func TestRun_slashStepIDIsEngineError(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "slash.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	graph := &spec.ProjectGraph{
		Tools:    map[string]*spec.ToolResource{"helper": helperTool()},
		Policies: map[string]*spec.PolicyResource{"default": defaultPolicy()},
		Workflows: map[string]*spec.WorkflowResource{
			"w": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "w"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{{ID: "nest/inner", Uses: "tool.helper.echo"}},
				},
			},
		},
	}
	started := time.Now().UTC()
	if err := st.StartRun(ctx, state.Run{
		RunID: "run-slash", WorkflowName: "w", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	ex := &Executor{
		Graph: graph, ProjectRoot: t.TempDir(),
		Tools: tools.NewRegistry(graph), Store: st, Trace: trace.NewRecorder(st),
	}
	err = ex.Run(ctx, RunInput{RunID: "run-slash", WorkflowName: "w", Env: "dev", StartedAt: started, Input: map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "must not contain '/'") {
		t.Fatalf("got %v", err)
	}
}

func TestRun_nestedAndSiblingCannotJointlyExceedCost(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "nest-cost.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{"helper": helperTool()},
		Policies: map[string]*spec.PolicyResource{
			"default": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindPolicy,
				Metadata:   spec.Metadata{Name: "default"},
				Spec:       spec.PolicySpec{Execution: &spec.PolicyExecution{MaxTotalCostUsd: 1.00}},
			},
		},
		Workflows: map[string]*spec.WorkflowResource{
			"child": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "child"},
				Spec: spec.WorkflowSpec{
					Policy: "default",
					Steps:  []spec.WorkflowStep{{ID: "inner", Uses: "tool.helper.echo", With: map[string]any{"which": "inner"}}},
				},
			},
			"parent": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "parent"},
				Spec: spec.WorkflowSpec{
					Policy: "default",
					Steps: []spec.WorkflowStep{
						{ID: "sib", Uses: "tool.helper.echo", With: map[string]any{"which": "sib"}, NeedsDeclared: true},
						{ID: "call", Workflow: "child", NeedsDeclared: true},
					},
				},
			},
		},
	}

	runID := "run-nest-cost"
	started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "parent", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	gate := newStartBarrier()
	ex := &Executor{
		Graph: graph, ProjectRoot: t.TempDir(),
		Tools: &tools.MockExecutor{Fn: func(ctx context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
			w := whichOf(req)
			if w == "sib" || w == "inner" {
				if !gate.Wait(ctx, 5*time.Second) {
					return tools.ToolCallResponse{}, fmt.Errorf("rendezvous timeout on %s", w)
				}
			}
			return tools.ToolCallResponse{Output: map[string]any{"echo": w}, Meta: tools.ToolCallMeta{CostUSD: 0.60}}, nil
		}},
		Store: st, Trace: trace.NewRecorder(st),
	}
	err = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "parent", Env: "dev", StartedAt: started, Input: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected joint $0.60+$0.60 to fail a $1.00 cap")
	}
	if !strings.Contains(err.Error(), "exceeds ceiling") {
		t.Fatalf("want ceiling error, got %v", err)
	}
}
