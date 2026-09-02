package mcpserver

import (
	"testing"

	"github.com/Terfyn/terfyn/internal/spec"
)

// toolWithOperations builds a closed-world tool (declared operations => closed manifest).
func toolWithOperations(name string, ops ...string) *spec.ToolResource {
	m := make(map[string]spec.ToolOperation, len(ops))
	for _, op := range ops {
		m[op] = spec.ToolOperation{Effects: []string{"example.effect"}}
	}
	return &spec.ToolResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindTool,
		Metadata:   spec.Metadata{Name: name},
		Spec:       spec.ToolSpec{Type: "mock", Operations: m},
	}
}

func agentGranting(name string, grants ...string) *spec.AgentResource {
	return &spec.AgentResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindAgent,
		Metadata:   spec.Metadata{Name: name},
		Spec:       spec.AgentSpec{Tools: grants},
	}
}

// The issue's "Done when": grants of two workspace ops ⇒ exactly those two tools appear, and
// nothing resembling write_file (an ungranted op) is reachable.
func TestCompile_ClosedWorldExactlyGrantedOps(t *testing.T) {
	g := &spec.ProjectGraph{
		Agents: map[string]*spec.AgentResource{
			"Coder": agentGranting("Coder", "tool.workspace.read_file", "tool.workspace.run_tests"),
		},
		Tools: map[string]*spec.ToolResource{
			"workspace": toolWithOperations("workspace", "read_file", "run_tests", "write_file"),
		},
	}
	cs, err := Compile(g, "Coder")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, op := range cs.Ops {
		got[op.MCPName] = true
	}
	want := map[string]bool{"workspace_read_file": true, "workspace_run_tests": true}
	if len(got) != len(want) {
		t.Fatalf("tools/list surface = %v, want %v", got, want)
	}
	for name := range want {
		if !got[name] {
			t.Fatalf("missing granted tool %q in %v", name, got)
		}
	}
	if got["workspace_write_file"] {
		t.Fatal("ungranted write_file must not appear in the compiled surface")
	}
}

func TestCompile_DuplicateGrantsCollapse(t *testing.T) {
	g := &spec.ProjectGraph{
		Agents: map[string]*spec.AgentResource{
			"A": agentGranting("A", "tool.workspace.read_file", "tool.workspace.read_file"),
		},
		Tools: map[string]*spec.ToolResource{"workspace": toolWithOperations("workspace", "read_file")},
	}
	cs, err := Compile(g, "A")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Ops) != 1 {
		t.Fatalf("duplicate grants must collapse to one op, got %d", len(cs.Ops))
	}
}

// A grant outside a *closed* manifest must be rejected — the per-run server can never advertise
// an operation the closed world does not contain.
func TestCompile_GrantOutsideClosedManifestRejected(t *testing.T) {
	g := &spec.ProjectGraph{
		Agents: map[string]*spec.AgentResource{
			"A": agentGranting("A", "tool.workspace.delete_everything"),
		},
		Tools: map[string]*spec.ToolResource{"workspace": toolWithOperations("workspace", "read_file")},
	}
	if _, err := Compile(g, "A"); err == nil {
		t.Fatal("a grant outside the closed manifest must be an error")
	}
}

// A grant on an open tool (no declared operations) passes through — open tools opt out of the
// manifest bound, consistent with closed-world semantics elsewhere.
func TestCompile_OpenToolGrantPassesThrough(t *testing.T) {
	g := &spec.ProjectGraph{
		Agents: map[string]*spec.AgentResource{"A": agentGranting("A", "tool.search.query")},
		Tools: map[string]*spec.ToolResource{
			"search": {Metadata: spec.Metadata{Name: "search"}, Spec: spec.ToolSpec{Type: "mock"}},
		},
	}
	cs, err := Compile(g, "A")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Ops) != 1 || cs.Ops[0].MCPName != "search_query" {
		t.Fatalf("open-tool grant should pass through, got %+v", cs.Ops)
	}
}

func TestCompile_UnknownAgent(t *testing.T) {
	if _, err := Compile(&spec.ProjectGraph{}, "Ghost"); err == nil {
		t.Fatal("compiling an unknown agent must error")
	}
}

func TestMCPToolName_DottedOperation(t *testing.T) {
	if got := mcpToolName("github", "pull_request.post_comment"); got != "github_pull_request_post_comment" {
		t.Fatalf("dotted op name = %q", got)
	}
}
