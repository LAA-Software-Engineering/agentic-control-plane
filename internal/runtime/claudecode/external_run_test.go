package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
	"github.com/Terfyn/terfyn/internal/tools"
	"github.com/Terfyn/terfyn/internal/trace"
)

// fakeExec records the granted calls the dispatcher routes to it.
type fakeExec struct{ calls []string }

func (f *fakeExec) Call(_ context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
	f.calls = append(f.calls, req.Uses)
	return tools.ToolCallResponse{Output: map[string]any{"read": req.With["path"]}}, nil
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

// mcpConfigEndpoint reads the --mcp-config file the driver wrote and returns the loopback URL and
// Authorization header the spawned agent would use.
func mcpConfigEndpoint(t *testing.T, argv []string) (url, auth string) {
	t.Helper()
	var path string
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--mcp-config" {
			path = argv[i+1]
		}
	}
	if path == "" {
		t.Fatal("no --mcp-config in argv")
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

// End-to-end without a real claude: the fake process reads the --mcp-config, authenticates to the
// per-run endpoint, drives a GRANTED tool (routed through policy to the executor) and an UNGRANTED
// one (refused by the closed world), then returns a success stream. Proves the whole composition:
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
	runner := func(_ context.Context, argv []string, _ string) (string, error) {
		url, auth := mcpConfigEndpoint(t, argv)
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
		return successStream, nil
	}

	got, run, err := ClaudeCodeRuntime{Run: runner}.RunExternalAgent(ctx, ExternalAgentRun{
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

// A budget breach fails closed even though the harness reported success.
func TestRunExternalAgent_budgetFailsClosed(t *testing.T) {
	ctx := context.Background()
	graph := reviewerGraph()
	eval := policy.NewEvaluator(graph, &spec.PolicySpec{Execution: &spec.PolicyExecution{MaxTotalCostUsd: 0.01}})
	runner := fakeRunner(successStream, nil, nil) // successStream carries total_cost_usd 0.0123 > 0.01
	_, _, err := ClaudeCodeRuntime{Run: runner}.RunExternalAgent(ctx, ExternalAgentRun{
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
