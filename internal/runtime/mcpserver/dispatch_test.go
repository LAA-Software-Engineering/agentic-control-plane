package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/tools"
)

// recordingExecutor records the call it received and returns a canned output.
type recordingExecutor struct {
	got tools.ToolCallRequest
}

func (r *recordingExecutor) Call(_ context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
	r.got = req
	return tools.ToolCallResponse{Output: map[string]any{"echoed": req.Uses}}, nil
}

func dispatchGraph() *spec.ProjectGraph {
	g := &spec.ProjectGraph{Tools: map[string]*spec.ToolResource{
		"workspace": toolWithOperations("workspace", "read_file"),
	}}
	g.Tools["workspace"].Spec.Safety = &spec.ToolSafety{
		Trusted:          spec.BoolPtr(true),
		SideEffects:      spec.BoolPtr(false),
		RequiresApproval: spec.BoolPtr(false),
	}
	return g
}

// A granted, manifest-declared op passes policy and reaches the executor.
func TestPolicyDispatcher_GrantedOpExecutes(t *testing.T) {
	g := dispatchGraph()
	exec := &recordingExecutor{}
	d := NewPolicyDispatcher(policy.NewEvaluator(g, nil), exec, policy.RunContext{})
	out, err := d.Call(context.Background(), "tool.workspace.read_file", map[string]any{"path": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if exec.got.Uses != "tool.workspace.read_file" {
		t.Fatalf("executor got %q", exec.got.Uses)
	}
	if out["echoed"] != "tool.workspace.read_file" {
		t.Fatalf("output not returned: %v", out)
	}
}

// The dispatcher routes through CheckToolCall: an op outside the closed manifest is denied by
// policy and never reaches the executor, even though the Server would only ever hand the
// dispatcher a granted uses string (defense in depth on the authority boundary).
func TestPolicyDispatcher_ClosedWorldDeniedBeforeExec(t *testing.T) {
	g := dispatchGraph()
	exec := &recordingExecutor{}
	d := NewPolicyDispatcher(policy.NewEvaluator(g, nil), exec, policy.RunContext{})
	_, err := d.Call(context.Background(), "tool.workspace.write_file", nil)
	if err == nil {
		t.Fatal("op outside the closed manifest must be denied by policy")
	}
	if exec.got.Uses != "" {
		t.Fatal("executor must not run when policy denies")
	}
}

func TestMCPConfigJSON_Stdio(t *testing.T) {
	b, err := MCPConfigJSON("terfyn", Transport{Command: "terfyn", Args: []string{"__mcp-serve"}})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]map[string]map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	entry := doc["mcpServers"]["terfyn"]
	if entry["type"] != "stdio" || entry["command"] != "terfyn" {
		t.Fatalf("stdio config = %s", b)
	}
}

func TestMCPConfigJSON_HTTP(t *testing.T) {
	b, err := MCPConfigJSON("terfyn", Transport{URL: "http://127.0.0.1:8765/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"type": "http"`) || !strings.Contains(string(b), "127.0.0.1:8765") {
		t.Fatalf("http config = %s", b)
	}
}

func TestMCPConfigJSON_RequiresTransport(t *testing.T) {
	if _, err := MCPConfigJSON("terfyn", Transport{}); err == nil {
		t.Fatal("an empty transport must error")
	}
}
