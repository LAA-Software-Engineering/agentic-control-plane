package agentcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
	"github.com/Terfyn/terfyn/internal/tools"
	"github.com/Terfyn/terfyn/internal/trace"
)

// fakeDriver is an AgentRuntime whose RunSession is a test-supplied function. It stands in for a
// real CLI driver (ClaudeCodeRuntime, GeminiRuntime, …) so the composition can be tested without a
// process: the function receives the RunSpec (including the per-run MCPConfig path) and returns the
// Session the driver would have parsed.
type fakeDriver struct {
	run func(ctx context.Context, spec RunSpec) (Session, error)
}

func (f fakeDriver) RunSession(ctx context.Context, spec RunSpec) (Session, error) {
	return f.run(ctx, spec)
}

// fakeExec records the granted calls the dispatcher routes to it.
type fakeExec struct{ calls []string }

func (f *fakeExec) Call(_ context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
	f.calls = append(f.calls, req.Uses)
	return tools.ToolCallResponse{Output: map[string]any{"read": req.With["path"]}}, nil
}

// fakeExec also satisfies mcpserver.ToolEnforcer so RunExternalAgent's fail-closed enforcement
// requirement is met; with no schemas/limits declared these are no-ops (#390).
func (f *fakeExec) ValidateInputSchema(string, map[string]any) error { return nil }
func (f *fakeExec) ResolveToolExecutionLimits(string) spec.ResolvedExecutionLimits {
	return spec.ResolveExecutionLimits(nil, nil, nil)
}

func reviewerGraph() *spec.ProjectGraph {
	ws := &spec.ToolResource{
		APIVersion: spec.APIVersionV0, Kind: spec.KindTool,
		Metadata: spec.Metadata{Name: "workspace"},
		Spec: spec.ToolSpec{Type: "mock", Operations: map[string]spec.ToolOperation{
			"read_file":  {Effects: []string{"workspace.read"}},
			"write_file": {Effects: []string{"workspace.write"}},
		}},
	}
	ws.Spec.Safety = &spec.ToolSafety{Trusted: spec.BoolPtr(true), SideEffects: spec.BoolPtr(false), RequiresApproval: spec.BoolPtr(false)}
	return &spec.ProjectGraph{
		Agents: map[string]*spec.AgentResource{
			"Reviewer": {
				APIVersion: spec.APIVersionV0, Kind: spec.KindAgent,
				Metadata: spec.Metadata{Name: "Reviewer"},
				Spec:     spec.AgentSpec{Model: "mock/gpt-4", Instructions: "review the change", Tools: []string{"tool.workspace.read_file"}},
			},
		},
		Tools: map[string]*spec.ToolResource{"workspace": ws},
	}
}

// mcpEndpointFromConfig reads the --mcp-config file the composition wrote and returns the loopback
// URL and Authorization header the spawned agent would use.
func mcpEndpointFromConfig(t *testing.T, path string) (url, auth string) {
	t.Helper()
	if path == "" {
		t.Fatal("empty MCPConfig path")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		MCPServers map[string]struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	s := doc.MCPServers["terfyn"]
	return s.URL, s.Headers["Authorization"]
}

// callMCP POSTs one tools/call and returns the decoded JSON-RPC response.
func callMCP(t *testing.T, url, auth, toolName string, args map[string]any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": toolName, "arguments": args},
	})
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Authorization", auth)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode MCP response (%d): %s", resp.StatusCode, raw)
	}
	return out
}

// End-to-end with a fake driver: the driver reads the per-run MCPConfig, authenticates to the
// endpoint, drives a GRANTED tool (routed through policy to the executor) and an UNGRANTED one
// (refused by the closed world), then returns a success Session. Proves the whole composition:
// grant compilation, authenticated transport, policy dispatch, trace, and budget.
func TestRunExternalAgent_endToEnd(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "run.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	const runID = "run-ext"
	if err := st.StartRun(ctx, state.Run{RunID: runID, WorkflowName: "review", Env: "dev", Status: "running", InputJSON: `{}`}); err != nil {
		t.Fatal(err)
	}
	rec := trace.NewRecorder(st)
	graph := reviewerGraph()
	exec := &fakeExec{}

	var grantedOK, ungrantedRefused bool
	driver := fakeDriver{run: func(_ context.Context, spec RunSpec) (Session, error) {
		url, auth := mcpEndpointFromConfig(t, spec.MCPConfig)
		// A granted op: routes through policy to the executor, isError=false.
		got := callMCP(t, url, auth, "workspace_read_file", map[string]any{"path": "main.go"})
		if res, ok := got["result"].(map[string]any); ok && res["isError"] == false {
			grantedOK = true
		}
		// An ungranted op: not in tools/list, refused before dispatch (closed world).
		bad := callMCP(t, url, auth, "workspace_write_file", map[string]any{})
		if bad["error"] != nil {
			ungrantedRefused = true
		}
		return Session{StopReason: StopSuccess, CostUSD: 0.0123, NumTurns: 1, Turns: []Turn{{Text: "done"}}}, nil
	}}

	got, run, err := RunExternalAgent(ctx, driver, ExternalAgentRun{
		Graph:     graph,
		Agent:     graph.Agents["Reviewer"],
		Eval:      policy.NewEvaluator(graph, nil),
		Exec:      exec,
		Recorder:  rec,
		RunID:     runID,
		Prompt:    "review it",
		Run:       policy.RunContext{},
		Limits:    Limits{MaxTurns: 8},
		ConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("RunExternalAgent: %v", err)
	}
	if !grantedOK {
		t.Fatal("granted tool call did not succeed through the MCP server")
	}
	if !ungrantedRefused {
		t.Fatal("ungranted tool call was not refused by the closed world")
	}
	if len(exec.calls) != 1 || exec.calls[0] != "tool.workspace.read_file" {
		t.Fatalf("executor calls = %v (only the granted op must reach it)", exec.calls)
	}
	if got.StopReason != StopSuccess {
		t.Fatalf("session stop = %v", got.StopReason)
	}

	// The run is auditable: tool_selection/tool_execution (from the dispatcher) + llm_completion
	// (from the session turns) all landed on the run's chain.
	events, err := st.ListTraceEventsByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, e := range events {
		kinds[e.Type]++
	}
	if kinds[string(trace.EventToolSelection)] == 0 || kinds[string(trace.EventToolExecution)] == 0 || kinds[string(trace.EventLLMCompletion)] == 0 {
		t.Fatalf("missing trace events: %v", kinds)
	}
	_ = run
}

// nonEnforcingExec forwards Call but does NOT implement mcpserver.ToolEnforcer, simulating a
// decorator that wraps the registry for Call only (cache/metrics/retry) — the exact way enforcement
// could be silently dropped.
type nonEnforcingExec struct{}

func (nonEnforcingExec) Call(_ context.Context, _ tools.ToolCallRequest) (tools.ToolCallResponse, error) {
	return tools.ToolCallResponse{Output: map[string]any{}}, nil
}

// TestRunExternalAgent_refusesNonEnforcingExecutor proves the production path fails CLOSED rather
// than running an external agent whose executor cannot enforce operation schema / byte limits — so
// wrapping or swapping the executor can never silently drop the #390 enforcement.
func TestRunExternalAgent_refusesNonEnforcingExecutor(t *testing.T) {
	ctx := context.Background()
	graph := reviewerGraph()
	driver := fakeDriver{run: func(_ context.Context, _ RunSpec) (Session, error) {
		t.Fatal("driver must not run when the executor cannot enforce")
		return Session{}, nil
	}}
	_, _, err := RunExternalAgent(ctx, driver, ExternalAgentRun{
		Graph:     graph,
		Agent:     graph.Agents["Reviewer"],
		Eval:      policy.NewEvaluator(graph, nil),
		Exec:      nonEnforcingExec{},
		RunID:     "r",
		Run:       policy.RunContext{},
		ConfigDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected RunExternalAgent to refuse a non-enforcing executor")
	}
	if !strings.Contains(err.Error(), "does not enforce") {
		t.Fatalf("expected a fail-closed enforcement error, got %v", err)
	}
}

// TestRunExternalAgent_foldsCostOnError proves a failed/max-turns external run still folds the
// session cost into the returned run context (so runs.total_cost_usd is not recorded as 0 for the
// runs most likely to have spent the most), while the run error still propagates (#400).
func TestRunExternalAgent_foldsCostOnError(t *testing.T) {
	ctx := context.Background()
	graph := reviewerGraph()
	driver := fakeDriver{run: func(_ context.Context, _ RunSpec) (Session, error) {
		return Session{StopReason: StopMaxTurns, CostUSD: 1.25, NumTurns: 8}, errors.New("max turns")
	}}
	_, run, err := RunExternalAgent(ctx, driver, ExternalAgentRun{
		Graph:     graph,
		Agent:     graph.Agents["Reviewer"],
		Eval:      policy.NewEvaluator(graph, nil),
		Exec:      &fakeExec{},
		RunID:     "r",
		Run:       policy.RunContext{},
		ConfigDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected the run error to propagate")
	}
	if run.AccumulatedCostUSD != 1.25 {
		t.Fatalf("session cost not folded on error: got %v, want 1.25", run.AccumulatedCostUSD)
	}
}

// TestRunExternalAgent_recordsOverspendOnError proves a failed run that also overspent the budget
// records a limit_hit and still returns the folded cost and the original run error (#400).
func TestRunExternalAgent_recordsOverspendOnError(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "run.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	const runID = "run-overspend"
	if err := st.StartRun(ctx, state.Run{RunID: runID, WorkflowName: "review", Env: "dev", Status: "running", InputJSON: `{}`}); err != nil {
		t.Fatal(err)
	}
	rec := trace.NewRecorder(st)
	graph := reviewerGraph()
	eval := policy.NewEvaluator(graph, &spec.PolicySpec{Execution: &spec.PolicyExecution{MaxTotalCostUsd: 0.5}})
	driver := fakeDriver{run: func(_ context.Context, _ RunSpec) (Session, error) {
		return Session{StopReason: StopMaxTurns, CostUSD: 1.25}, errors.New("max turns")
	}}
	_, run, runErr := RunExternalAgent(ctx, driver, ExternalAgentRun{
		Graph: graph, Agent: graph.Agents["Reviewer"], Eval: eval, Exec: &fakeExec{},
		Recorder: rec, RunID: runID, Run: policy.RunContext{}, ConfigDir: t.TempDir(),
	})
	if runErr == nil || !strings.Contains(runErr.Error(), "max turns") {
		t.Fatalf("expected the max-turns run error, got %v", runErr)
	}
	if run.AccumulatedCostUSD != 1.25 {
		t.Fatalf("cost not folded on error: got %v", run.AccumulatedCostUSD)
	}
	events, err := st.ListTraceEventsByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	var sawLimit bool
	for _, e := range events {
		if e.Type == string(trace.EventLimitHit) {
			sawLimit = true
		}
	}
	if !sawLimit {
		t.Fatal("expected a limit_hit event for the budget overspend on a failed run")
	}
}

// A budget breach fails closed even though the driver reported success.
func TestRunExternalAgent_budgetFailsClosed(t *testing.T) {
	ctx := context.Background()
	graph := reviewerGraph()
	eval := policy.NewEvaluator(graph, &spec.PolicySpec{Execution: &spec.PolicyExecution{MaxTotalCostUsd: 0.01}})
	driver := fakeDriver{run: func(_ context.Context, _ RunSpec) (Session, error) {
		return Session{StopReason: StopSuccess, CostUSD: 0.0123}, nil // 0.0123 > 0.01
	}}
	_, _, err := RunExternalAgent(ctx, driver, ExternalAgentRun{
		Graph:     graph,
		Agent:     graph.Agents["Reviewer"],
		Eval:      eval,
		Exec:      &fakeExec{},
		RunID:     "r",
		Run:       policy.RunContext{},
		ConfigDir: t.TempDir(),
	})
	if d, ok := policy.AsDenied(err); !ok || d.Reason != policy.ReasonMaxCost {
		t.Fatalf("expected a max_cost budget denial, got %v", err)
	}
}
