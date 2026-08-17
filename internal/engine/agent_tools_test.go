package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestAgentMaxIterations(t *testing.T) {
	t.Parallel()
	if got := agentMaxIterations(nil); got != defaultAgentMaxIterations {
		t.Fatalf("nil agent = %d", got)
	}
	if got := agentMaxIterations(&spec.AgentResource{}); got != defaultAgentMaxIterations {
		t.Fatalf("unset = %d", got)
	}
	if got := agentMaxIterations(&spec.AgentResource{Spec: spec.AgentSpec{Constraints: &spec.AgentConstraints{MaxIterations: 0}}}); got != defaultAgentMaxIterations {
		t.Fatalf("zero = %d", got)
	}
	if got := agentMaxIterations(&spec.AgentResource{Spec: spec.AgentSpec{Constraints: &spec.AgentConstraints{MaxIterations: 2}}}); got != 2 {
		t.Fatalf("explicit = %d", got)
	}
	if got := agentMaxIterations(&spec.AgentResource{Spec: spec.AgentSpec{Constraints: &spec.AgentConstraints{MaxIterations: 99}}}); got != hardAgentMaxIterations {
		t.Fatalf("hard cap = %d", got)
	}
}

func TestResolveAgentToolCall(t *testing.T) {
	t.Parallel()
	declared := map[string]struct{}{"helper": {}, "docs": {}}
	tests := []struct {
		name    string
		given   string
		want    string
		wantErr string
	}{
		{name: "bare tool", given: "helper", want: "tool.helper.default"},
		{name: "tool.op", given: "helper.echo", want: "tool.helper.echo"},
		{name: "full uses", given: "tool.helper.echo", want: "tool.helper.echo"},
		{name: "tool.name only", given: "tool.helper", want: "tool.helper.default"},
		{name: "undeclared", given: "ghost", wantErr: "not declared"},
		{name: "empty", given: "  ", wantErr: "missing name"},
		{name: "undeclared full uses", given: "tool.ghost.default", wantErr: "not declared"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveAgentToolCall(tc.given, declared)
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

func TestAgentToolDefs(t *testing.T) {
	t.Parallel()
	e := &Executor{Graph: &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"helper": {Metadata: spec.Metadata{Name: "helper"}, Spec: spec.ToolSpec{Type: "mock"}},
		},
	}}
	defs, err := e.agentToolDefs(&spec.AgentResource{
		Metadata: spec.Metadata{Name: "reviewer"},
		Spec:     spec.AgentSpec{Tools: []string{"helper", "helper", ""}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Name != "helper" {
		t.Fatalf("defs %+v", defs)
	}
	if string(defs[0].Parameters) != string(defaultAgentToolParameters) {
		t.Fatalf("params %s", defs[0].Parameters)
	}
	_, err = e.agentToolDefs(&spec.AgentResource{
		Metadata: spec.Metadata{Name: "reviewer"},
		Spec:     spec.AgentSpec{Tools: []string{"ghost"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("got %v", err)
	}
}
