package spec

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateProjectGraph_metadataKeyMismatch(t *testing.T) {
	g := &ProjectGraph{
		Agents: map[string]*AgentResource{
			"foo": {
				Kind:     KindAgent,
				Metadata: Metadata{Name: "bar"},
				Spec:     AgentSpec{},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), `map key "foo"`) {
		t.Fatalf("expected map key mismatch error, got %v", err)
	}
}

func TestValidateProjectGraph_negativePolicyBudgets(t *testing.T) {
	g := &ProjectGraph{
		Policies: map[string]*PolicyResource{
			"p": {
				Kind:     KindPolicy,
				Metadata: Metadata{Name: "p"},
				Spec: PolicySpec{
					Execution: &PolicyExecution{
						MaxWallClockSeconds: -1,
						MaxTotalCostUsd:     -0.01,
					},
				},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil {
		t.Fatal("expected errors")
	}
	if !strings.Contains(err.Error(), "maxWallClockSeconds") || !strings.Contains(err.Error(), "maxTotalCostUsd") {
		t.Fatalf("expected budget errors: %v", err)
	}
}

func TestValidateProjectGraph_workflowStepBothAgentAndUses(t *testing.T) {
	g := &ProjectGraph{
		Agents: map[string]*AgentResource{
			"a": {Kind: KindAgent, Metadata: Metadata{Name: "a"}, Spec: AgentSpec{}},
		},
		Tools: map[string]*ToolResource{
			"t": {Kind: KindTool, Metadata: Metadata{Name: "t"}, Spec: ToolSpec{Type: "native"}},
		},
		Workflows: map[string]*WorkflowResource{
			"w": {
				Kind:     KindWorkflow,
				Metadata: Metadata{Name: "w"},
				Spec: WorkflowSpec{
					Steps: []WorkflowStep{
						{ID: "s1", Agent: "a", Uses: "tool.t.x"},
					},
				},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "both agent and uses") {
		t.Fatalf("expected both agent and uses error, got %v", err)
	}
}

func TestValidateProjectGraph_workflowStepNeitherAgentNorUses(t *testing.T) {
	g := &ProjectGraph{
		Workflows: map[string]*WorkflowResource{
			"w": {
				Kind:     KindWorkflow,
				Metadata: Metadata{Name: "w"},
				Spec: WorkflowSpec{
					Steps: []WorkflowStep{{ID: "s1"}},
				},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "exactly one of agent or uses") {
		t.Fatalf("expected step shape error, got %v", err)
	}
}

func TestValidateProjectGraph_agentMissingTool(t *testing.T) {
	g := &ProjectGraph{
		Agents: map[string]*AgentResource{
			"a": {
				Kind:     KindAgent,
				Metadata: Metadata{Name: "a"},
				Spec:     AgentSpec{Tools: []string{"ghost"}},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	var mr *MissingRefError
	if !errors.As(err, &mr) {
		t.Fatalf("want *MissingRefError, got %T: %v", err, err)
	}
	if mr.Missing.Name != "ghost" || mr.Missing.Kind != KindTool {
		t.Fatalf("Missing = %v", mr.Missing)
	}
}

func TestValidateProjectGraph_missingSchemaFile(t *testing.T) {
	root := t.TempDir()
	g := &ProjectGraph{
		Agents: map[string]*AgentResource{
			"a": {
				Kind:     KindAgent,
				Metadata: Metadata{Name: "a"},
				Spec: AgentSpec{
					Input: &AgentIO{Schema: "./schemas/nope.json"},
				},
			},
		},
	}
	err := ValidateProjectGraph(g, root)
	if err == nil || !strings.Contains(err.Error(), "nope.json") {
		t.Fatalf("expected schema path error, got %v", err)
	}
}

func TestValidateProjectGraph_schemaFileExists(t *testing.T) {
	root := t.TempDir()
	schemaDir := filepath.Join(root, "schemas")
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(schemaDir, "in.json")
	if err := os.WriteFile(p, []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &ProjectGraph{
		Agents: map[string]*AgentResource{
			"a": {
				Kind:     KindAgent,
				Metadata: Metadata{Name: "a"},
				Spec: AgentSpec{
					Input: &AgentIO{Schema: "./schemas/in.json"},
				},
			},
		},
	}
	if err := ValidateProjectGraph(g, root); err != nil {
		t.Fatal(err)
	}
}

func TestValidateProjectGraph_duplicateApprovalActions(t *testing.T) {
	g := &ProjectGraph{
		Policies: map[string]*PolicyResource{
			"p": {
				Kind:     KindPolicy,
				Metadata: Metadata{Name: "p"},
				Spec: PolicySpec{
					Approvals: &PolicyApprovals{
						RequiredFor: []string{"tool.x.merge", "tool.x.merge"},
					},
				},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "duplicate approvals") {
		t.Fatalf("expected duplicate approvals error, got %v", err)
	}
}

func TestValidateProjectGraph_defaultsRuntimeUnknown(t *testing.T) {
	g := &ProjectGraph{
		Spec: ProjectSpec{
			Defaults: &ProjectDefaults{Runtime: "k8s"},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), `defaults.runtime "k8s"`) {
		t.Fatalf("expected defaults.runtime error, got %v", err)
	}
}

func TestValidateProjectGraph_agentRuntimeUnknown(t *testing.T) {
	g := &ProjectGraph{
		Agents: map[string]*AgentResource{
			"a": {
				Kind:     KindAgent,
				Metadata: Metadata{Name: "a"},
				Spec:     AgentSpec{Runtime: "remote"},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), `Agent/a: spec.runtime "remote"`) {
		t.Fatalf("expected agent runtime error, got %v", err)
	}
}

func TestValidateProjectGraph_workflowRuntimeUnknown(t *testing.T) {
	g := &ProjectGraph{
		Workflows: map[string]*WorkflowResource{
			"w": {
				Kind:     KindWorkflow,
				Metadata: Metadata{Name: "w"},
				Spec:     WorkflowSpec{Runtime: "lambda"},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), `Workflow/w: spec.runtime "lambda"`) {
		t.Fatalf("expected workflow runtime error, got %v", err)
	}
}

func TestValidateProjectGraph_mcpMissingTransport(t *testing.T) {
	g := &ProjectGraph{
		Tools: map[string]*ToolResource{
			"x": {
				Kind:     KindTool,
				Metadata: Metadata{Name: "x"},
				Spec: ToolSpec{
					Type: "mcp",
					MCP:  &ToolMCP{Command: "npx"},
				},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "spec.mcp.transport is required") {
		t.Fatalf("expected mcp transport error, got %v", err)
	}
}

func TestValidateProjectGraph_mcpStdioWithURL(t *testing.T) {
	g := &ProjectGraph{
		Tools: map[string]*ToolResource{
			"x": {
				Kind:     KindTool,
				Metadata: Metadata{Name: "x"},
				Spec: ToolSpec{
					Type: "mcp",
					MCP: &ToolMCP{
						Transport: "stdio",
						Command:   "npx",
						URL:       "http://bad",
					},
				},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "must not set url") {
		t.Fatalf("expected stdio/url conflict, got %v", err)
	}
}

func TestValidateProjectGraph_mcpHTTPWithCommand(t *testing.T) {
	g := &ProjectGraph{
		Tools: map[string]*ToolResource{
			"x": {
				Kind:     KindTool,
				Metadata: Metadata{Name: "x"},
				Spec: ToolSpec{
					Type: "mcp",
					MCP: &ToolMCP{
						Transport: "http",
						URL:       "https://example.com/mcp",
						Command:   "npx",
					},
				},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "must not set command") {
		t.Fatalf("expected http/command conflict, got %v", err)
	}
}

func TestValidateProjectGraph_mcpHTTPMissingURL(t *testing.T) {
	g := &ProjectGraph{
		Tools: map[string]*ToolResource{
			"x": {
				Kind:     KindTool,
				Metadata: Metadata{Name: "x"},
				Spec: ToolSpec{
					Type: "mcp",
					MCP: &ToolMCP{
						Transport: "http",
					},
				},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "http transport requires url") {
		t.Fatalf("expected mcp http url error, got %v", err)
	}
}

func TestValidateProjectGraph_toolSafetyEmptyBlock(t *testing.T) {
	g := &ProjectGraph{
		Tools: map[string]*ToolResource{
			"h": {
				Kind:     KindTool,
				Metadata: Metadata{Name: "h"},
				Spec: ToolSpec{
					Type:   "native",
					Safety: &ToolSafety{},
				},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "at least one of trusted") {
		t.Fatalf("expected empty safety error, got %v", err)
	}
}

func TestValidateProjectGraph_runtimeLocalAccepted(t *testing.T) {
	g := &ProjectGraph{
		Spec: ProjectSpec{
			Defaults: &ProjectDefaults{Runtime: "local"},
		},
		Agents: map[string]*AgentResource{
			"a": {
				Kind:     KindAgent,
				Metadata: Metadata{Name: "a"},
				Spec:     AgentSpec{Runtime: "local"},
			},
		},
		Workflows: map[string]*WorkflowResource{
			"w": {
				Kind:     KindWorkflow,
				Metadata: Metadata{Name: "w"},
				Spec:     WorkflowSpec{Runtime: "local"},
			},
		},
	}
	if err := ValidateProjectGraph(g, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
