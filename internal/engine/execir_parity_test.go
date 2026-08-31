package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/terfyn/internal/models"
	"github.com/LAA-Software-Engineering/terfyn/internal/policy"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
	"github.com/LAA-Software-Engineering/terfyn/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/terfyn/internal/tools"
	"github.com/LAA-Software-Engineering/terfyn/internal/trace"
)

// scenarioResult captures the observable outcome of one run for differential
// comparison between the DAG and execir paths (issue #257).
type scenarioResult struct {
	status string
	output map[string]any
	cost   float64
	denied bool
	events []string // projected (stepID|type|key) multiset, sorted
	runErr error
}

// runScenario runs one workflow through the engine once, against a fresh store,
// and returns its observable outcome. useExecIR toggles the execir path.
func runScenario(t *testing.T, graph *spec.ProjectGraph, wfName, inputJSON string, approvals []string, useExecIR bool) scenarioResult {
	t.Helper()
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "run.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	runID := "run-1"
	started := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: wfName, Env: "dev", Status: "running", StartedAt: started, InputJSON: inputJSON,
	}); err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
		t.Fatal(err)
	}

	ex := &Executor{
		Graph: graph,
		Tools: tools.NewRegistry(graph),
		// One mock client per run; agents with no output schema accept any JSON object.
		ModelResolve: func(string) (models.ModelClient, string, error) {
			return &models.MockClient{Content: `{"ok":true}`, Meta: &models.GenerateMeta{CostUSD: 0.01}}, "gpt-4", nil
		},
		Store: st,
		Trace: trace.NewRecorder(st),
		Now:   func() time.Time { return started },
	}
	runErr := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: wfName, Env: "dev", StartedAt: started,
		Input: input, ApprovedActions: approvals, UseExecIR: useExecIR,
	})

	run, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := st.ListTraceEventsByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	res := scenarioResult{status: run.Status, cost: run.TotalCostUSD, events: projectEvents(events), runErr: runErr}
	if run.OutputJSON != "" {
		_ = json.Unmarshal([]byte(run.OutputJSON), &res.output)
	}
	if _, ok := policy.AsDenied(runErr); ok {
		res.denied = true
	}
	return res
}

// projectEvents reduces trace events to an order-independent multiset of
// (stepID|type|salient data) so the comparison is robust to the nondeterministic
// completion order both paths exhibit under concurrency.
func projectEvents(events []state.TraceEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		var data map[string]any
		_ = json.Unmarshal([]byte(e.DataJSON), &data)
		key := ""
		for _, field := range []string{"uses", "tool", "reason", "kind"} {
			if v, ok := data[field]; ok {
				key += fmt.Sprintf("|%s=%v", field, v)
			}
		}
		if v, ok := data["success"]; ok {
			key += fmt.Sprintf("|success=%v", v)
		}
		out = append(out, e.StepID+"|"+e.Type+key)
	}
	sort.Strings(out)
	return out
}

// assertParity fails when the two paths diverge on any observable axis.
func assertParity(t *testing.T, dag, exec scenarioResult) {
	t.Helper()
	if dag.status != exec.status {
		t.Fatalf("status divergence: dag=%q execir=%q (dagErr=%v execErr=%v)", dag.status, exec.status, dag.runErr, exec.runErr)
	}
	if dag.cost != exec.cost {
		t.Fatalf("cost divergence: dag=%v execir=%v", dag.cost, exec.cost)
	}
	if fmt.Sprint(dag.output) != fmt.Sprint(exec.output) {
		t.Fatalf("output divergence:\n dag=  %v\n execir=%v", dag.output, exec.output)
	}
	if dag.denied != exec.denied {
		t.Fatalf("denial divergence: dag=%v execir=%v", dag.denied, exec.denied)
	}
	if fmt.Sprint(dag.events) != fmt.Sprint(exec.events) {
		t.Fatalf("trace divergence:\n dag=  %v\n execir=%v", dag.events, exec.events)
	}
}

// --- fixtures ---------------------------------------------------------------

func basePolicies(approvals *spec.PolicyApprovals) map[string]*spec.PolicyResource {
	return map[string]*spec.PolicyResource{
		"default": {
			APIVersion: spec.APIVersionV0, Kind: spec.KindPolicy, Metadata: spec.Metadata{Name: "default"},
			Spec: spec.PolicySpec{Approvals: approvals},
		},
	}
}

func nativeTool(name string) *spec.ToolResource {
	return &spec.ToolResource{
		APIVersion: spec.APIVersionV0, Kind: spec.KindTool, Metadata: spec.Metadata{Name: name},
		Spec: spec.ToolSpec{Type: "native", Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(false)}},
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func agentRes(name string) *spec.AgentResource {
	return &spec.AgentResource{
		APIVersion: spec.APIVersionV0, Kind: spec.KindAgent, Metadata: spec.Metadata{Name: name},
		Spec: spec.AgentSpec{Model: "mock/gpt-4", Instructions: "Return JSON."},
	}
}

// TestExecIRParity_Sequential: tool -> agent -> tool, straight-line.
func TestExecIRParity_Sequential(t *testing.T) {
	t.Parallel()
	graph := &spec.ProjectGraph{
		Tools:    map[string]*spec.ToolResource{"helper": nativeTool("helper")},
		Agents:   map[string]*spec.AgentResource{"reviewer": agentRes("reviewer")},
		Policies: basePolicies(nil),
		Workflows: map[string]*spec.WorkflowResource{
			"demo": {
				APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "demo"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{
						{ID: "fetch", Uses: "tool.helper.echo", With: map[string]any{"topic": "${input.topic}"}},
						{ID: "review", Agent: "reviewer", With: map[string]any{"echo": "${steps.fetch.output.echo}"}},
						{ID: "post", Uses: "tool.helper.echo", With: map[string]any{"note": "${steps.review.output.ok}"}},
					},
					Output: &spec.WorkflowOutput{Value: map[string]any{
						"topic":  "${input.topic}",
						"review": "${steps.review.output}",
					}},
				},
			},
		},
	}
	dag := runScenario(t, graph, "demo", `{"topic":"agents"}`, nil, false)
	exec := runScenario(t, graph, "demo", `{"topic":"agents"}`, nil, true)
	if dag.status != "succeeded" {
		t.Fatalf("dag did not succeed: %q err=%v", dag.status, dag.runErr)
	}
	assertParity(t, dag, exec)
}

// TestExecIRParity_Truncation: a byte-limited tool input is truncated identically
// on both paths, because truncation lives in enforceToolInput inside the shared
// runToolStep (#117/#204). Both must succeed with byte-identical output.
func TestExecIRParity_Truncation(t *testing.T) {
	t.Parallel()
	graph := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{Limits: &spec.ExecutionLimits{
			MaxToolInputBytes:     64,
			ToolInputExceedPolicy: spec.LimitExceedTruncate,
		}},
		Tools:    map[string]*spec.ToolResource{"helper": nativeTool("helper")},
		Policies: basePolicies(nil),
		Workflows: map[string]*spec.WorkflowResource{
			"trunc": {
				APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "trunc"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{
						{ID: "big", Uses: "tool.helper.echo", With: map[string]any{"blob": "${input.blob}"}},
					},
					Output: &spec.WorkflowOutput{Value: map[string]any{"echoed": "${steps.big.output}"}},
				},
			},
		},
	}
	input := `{"blob":"` + repeat("x", 400) + `"}`
	dag := runScenario(t, graph, "trunc", input, nil, false)
	exec := runScenario(t, graph, "trunc", input, nil, true)
	if dag.status != "succeeded" {
		t.Fatalf("dag did not succeed: %q err=%v", dag.status, dag.runErr)
	}
	assertParity(t, dag, exec)
}

// TestExecIRParity_GeneralDAG: A,B roots; C[A]; D[A,B]; E[C] — the join-accuracy
// graph Fork cannot express. Both paths must complete with identical output/cost.
func TestExecIRParity_GeneralDAG(t *testing.T) {
	t.Parallel()
	step := func(id string, needs ...string) spec.WorkflowStep {
		return spec.WorkflowStep{ID: id, Agent: "ag", Needs: needs, NeedsDeclared: true, With: map[string]any{"id": id}}
	}
	graph := &spec.ProjectGraph{
		Agents:   map[string]*spec.AgentResource{"ag": agentRes("ag")},
		Policies: basePolicies(nil),
		Workflows: map[string]*spec.WorkflowResource{
			"dag": {
				APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "dag"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{
						step("a"), step("b"), step("c", "a"), step("d", "a", "b"), step("e", "c"),
					},
					Output: &spec.WorkflowOutput{Value: map[string]any{
						"d": "${steps.d.output}",
						"e": "${steps.e.output}",
					}},
				},
			},
		},
	}
	dag := runScenario(t, graph, "dag", `{}`, nil, false)
	exec := runScenario(t, graph, "dag", `{}`, nil, true)
	if dag.status != "succeeded" {
		t.Fatalf("dag did not succeed: %q err=%v", dag.status, dag.runErr)
	}
	if exec.status != "succeeded" {
		t.Fatalf("execir did not succeed: %q err=%v", exec.status, exec.runErr)
	}
	assertParity(t, dag, exec)
}

// TestExecIRParity_HardDenial: a tool with a closed-empty capability manifest
// (operations declared but empty, #204) denies EVERY operation unconditionally —
// a hard fail-closed denial that binds before any approval and does NOT suspend
// (unlike approvals.requiredFor, which is HITL/suspend and out of Phase 1's bar).
// Both paths must fail-closed with the same denial (policy CheckToolCall runs
// inside the shared runToolStep).
func TestExecIRParity_HardDenial(t *testing.T) {
	t.Parallel()
	closedTool := nativeTool("helper")
	closedTool.Spec.Operations = map[string]spec.ToolOperation{}
	closedTool.Spec.OperationsDeclared = true
	graph := &spec.ProjectGraph{
		Tools:    map[string]*spec.ToolResource{"helper": closedTool},
		Policies: basePolicies(nil),
		Workflows: map[string]*spec.WorkflowResource{
			"gated": {
				APIVersion: spec.APIVersionV0, Kind: spec.KindWorkflow, Metadata: spec.Metadata{Name: "gated"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{
						{ID: "write", Uses: "tool.helper.echo", With: map[string]any{"x": "1"}},
					},
				},
			},
		},
	}
	dag := runScenario(t, graph, "gated", `{}`, nil, false)
	exec := runScenario(t, graph, "gated", `{}`, nil, true)
	if dag.status != "failed" || !dag.denied {
		t.Fatalf("dag should deny+fail, got status=%q denied=%v err=%v", dag.status, dag.denied, dag.runErr)
	}
	assertParity(t, dag, exec)
}
