package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/spec"
)

func TestAgentMaxIterations(t *testing.T) {
	t.Parallel()
	if got := agentMaxIterations(nil); got != spec.DefaultAgentMaxIterations {
		t.Fatalf("nil agent = %d", got)
	}
	if got := agentMaxIterations(&spec.AgentResource{}); got != spec.DefaultAgentMaxIterations {
		t.Fatalf("unset = %d", got)
	}
	if got := agentMaxIterations(&spec.AgentResource{Spec: spec.AgentSpec{Constraints: &spec.AgentConstraints{MaxIterations: 0}}}); got != spec.DefaultAgentMaxIterations {
		t.Fatalf("zero = %d", got)
	}
	if got := agentMaxIterations(&spec.AgentResource{Spec: spec.AgentSpec{Constraints: &spec.AgentConstraints{MaxIterations: 2}}}); got != 2 {
		t.Fatalf("explicit = %d", got)
	}
	if got := agentMaxIterations(&spec.AgentResource{Spec: spec.AgentSpec{Constraints: &spec.AgentConstraints{MaxIterations: 99}}}); got != spec.HardAgentMaxIterations {
		t.Fatalf("hard cap = %d", got)
	}
}

func TestResolveAgentToolCall(t *testing.T) {
	t.Parallel()
	advertised := map[string]string{
		"helper": "tool.helper.default",
		"docs":   "tool.docs.default",
	}
	tests := []struct {
		name    string
		given   string
		want    string
		wantErr string
	}{
		{name: "bare tool", given: "helper", want: "tool.helper.default"},
		{name: "tool.op", given: "helper.echo", wantErr: "not declared"},
		{name: "full uses", given: "tool.helper.echo", wantErr: "not declared"},
		{name: "native shell op", given: "helper.command.run", wantErr: "not declared"},
		{name: "http method.path", given: "helper.delete.users", wantErr: "not declared"},
		{name: "tool.name only", given: "tool.helper", wantErr: "not declared"},
		{name: "undeclared", given: "ghost", wantErr: "not declared"},
		{name: "empty", given: "  ", wantErr: "missing name"},
		{name: "undeclared full uses", given: "tool.ghost.default", wantErr: "not declared"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveAgentToolCall(tc.given, advertised)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestParseToolCallArgs(t *testing.T) {
	t.Parallel()
	got, err := parseToolCallArgs(nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("nil args %+v %v", got, err)
	}
	got, err = parseToolCallArgs(json.RawMessage(`{"q":"go"}`))
	if err != nil || got["q"] != "go" {
		t.Fatalf("object %+v %v", got, err)
	}
	_, err = parseToolCallArgs(json.RawMessage(`[]`))
	if err == nil || !strings.Contains(err.Error(), "must be a JSON object") {
		t.Fatalf("array err %v", err)
	}
}

func TestEncodeToolResultContent(t *testing.T) {
	t.Parallel()
	if got := encodeToolResultContent(nil); got != "{}" {
		t.Fatalf("nil %q", got)
	}
	if got := encodeToolResultContent(map[string]any{"uses": "tool.helper.default"}); got != `{"uses":"tool.helper.default"}` {
		t.Fatalf("got %s", got)
	}
}

func TestAdvertisedAgentTools(t *testing.T) {
	t.Parallel()
	e := &Executor{Graph: &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"helper": {Metadata: spec.Metadata{Name: "helper"}, Spec: spec.ToolSpec{Type: "mock"}},
			"shell":  {Metadata: spec.Metadata{Name: "shell"}, Spec: spec.ToolSpec{Type: "native"}},
		},
	}}
	defs, uses, err := e.advertisedAgentTools(&spec.AgentResource{
		Metadata: spec.Metadata{Name: "reviewer"},
		Spec:     spec.AgentSpec{Tools: []string{"helper", "helper", "", "shell"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 2 || defs[0].Name != "helper" || defs[1].Name != "shell" {
		t.Fatalf("defs %+v", defs)
	}
	if string(defs[0].Parameters) != string(defaultAgentToolParameters) {
		t.Fatalf("params %s", defs[0].Parameters)
	}
	if uses["helper"] != "tool.helper.default" {
		t.Fatalf("mock uses %q", uses["helper"])
	}
	if uses["shell"] != "tool.shell.echo" {
		t.Fatalf("native uses %q", uses["shell"])
	}
	_, _, err = e.advertisedAgentTools(&spec.AgentResource{
		Metadata: spec.Metadata{Name: "reviewer"},
		Spec:     spec.AgentSpec{Tools: []string{"ghost"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("got %v", err)
	}

	e.Graph.Tools["api"] = &spec.ToolResource{Metadata: spec.Metadata{Name: "api"}, Spec: spec.ToolSpec{Type: "http"}}
	_, _, err = e.advertisedAgentTools(&spec.AgentResource{
		Metadata: spec.Metadata{Name: "reviewer"},
		Spec:     spec.AgentSpec{Tools: []string{"api"}},
	})
	if err == nil || !strings.Contains(err.Error(), "no default operation") {
		t.Fatalf("bare http tool err %v", err)
	}
	defs, uses, err = e.advertisedAgentTools(&spec.AgentResource{
		Metadata: spec.Metadata{Name: "reviewer"},
		Spec:     spec.AgentSpec{Tools: []string{"tool.api.get.users"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Name != "api" || uses["api"] != "tool.api.get.users" {
		t.Fatalf("pinned http defs=%+v uses=%+v", defs, uses)
	}
	_, _, err = e.advertisedAgentTools(&spec.AgentResource{
		Metadata: spec.Metadata{Name: "reviewer"},
		Spec:     spec.AgentSpec{Tools: []string{"tool.api.default"}},
	})
	if err == nil || !strings.Contains(err.Error(), "no default operation") {
		t.Fatalf("pinned http default err %v", err)
	}
	// Multiple operations on one tool advertise as distinct per-operation tool-defs (#291).
	defs, uses, err = e.advertisedAgentTools(&spec.AgentResource{
		Metadata: spec.Metadata{Name: "reviewer"},
		Spec:     spec.AgentSpec{Tools: []string{"shell", "tool.shell.command.run"}},
	})
	if err != nil {
		t.Fatalf("multi-op advertise err %v", err)
	}
	if len(defs) != 2 || defs[0].Name != "shell.echo" || defs[1].Name != "shell.command.run" {
		t.Fatalf("multi-op defs %+v", defs)
	}
	if uses["shell.echo"] != "tool.shell.echo" || uses["shell.command.run"] != "tool.shell.command.run" {
		t.Fatalf("multi-op uses %+v", uses)
	}
}

// TestAgentToolCapabilityBoundary is the #291/#292 security property: an operation
// the agent did not advertise is denied at resolution, no matter what the model
// asks for. The Implementer holds three operations on one `workspace` tool; the
// Reviewer holds only read_file + run_tests. The Reviewer's attempt to call
// write_file is refused because it is outside its declared capability — the
// capability boundary is the control, not the prompt.
func TestAgentToolCapabilityBoundary(t *testing.T) {
	t.Parallel()
	e := &Executor{Graph: &spec.ProjectGraph{Tools: map[string]*spec.ToolResource{
		"workspace": {Metadata: spec.Metadata{Name: "workspace"}, Spec: spec.ToolSpec{Type: "mock"}},
	}}}

	implementer := &spec.AgentResource{Metadata: spec.Metadata{Name: "implementer"}, Spec: spec.AgentSpec{
		Tools: []string{"tool.workspace.read_file", "tool.workspace.write_file", "tool.workspace.run_tests"},
	}}
	reviewer := &spec.AgentResource{Metadata: spec.Metadata{Name: "reviewer"}, Spec: spec.AgentSpec{
		Tools: []string{"tool.workspace.read_file", "tool.workspace.run_tests"},
	}}

	_, implUses, err := e.advertisedAgentTools(implementer)
	if err != nil {
		t.Fatalf("implementer advertise: %v", err)
	}
	// The Implementer CAN invoke write_file — it is advertised and maps to its uses.
	uses, err := resolveAgentToolCall("workspace.write_file", implUses)
	if err != nil || uses != "tool.workspace.write_file" {
		t.Fatalf("implementer write_file should resolve, got uses=%q err=%v", uses, err)
	}

	_, revUses, err := e.advertisedAgentTools(reviewer)
	if err != nil {
		t.Fatalf("reviewer advertise: %v", err)
	}
	// The Reviewer read_file/run_tests resolve; write_file is DENIED (not advertised).
	if _, err := resolveAgentToolCall("workspace.read_file", revUses); err != nil {
		t.Fatalf("reviewer read_file should resolve, got %v", err)
	}
	if _, err := resolveAgentToolCall("workspace.write_file", revUses); err == nil {
		t.Fatalf("reviewer write_file MUST be denied at the capability boundary, but it resolved")
	}
	// A wholly unknown operation is likewise denied.
	if _, err := resolveAgentToolCall("workspace.delete_repo", revUses); err == nil {
		t.Fatalf("an unadvertised operation must be denied")
	}
}
