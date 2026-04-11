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
