package claudecode

import (
	"context"

	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/tools"
)

// fakeExec records the granted calls the dispatcher routes to it (shared by the runtime and S9
// live tests). The composition itself is covered in internal/runtime/agentcli.
type fakeExec struct{ calls []string }

func (f *fakeExec) Call(_ context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
	f.calls = append(f.calls, req.Uses)
	return tools.ToolCallResponse{Output: map[string]any{"read": req.With["path"]}}, nil
}

// fakeExec satisfies mcpserver.ToolEnforcer so RunExternalAgent's fail-closed enforcement is met
// (no-op with no schemas/limits declared) (#390).
func (f *fakeExec) ValidateInputSchema(string, map[string]any) error { return nil }
func (f *fakeExec) ResolveToolExecutionLimits(string) spec.ResolvedExecutionLimits {
	return spec.ResolveExecutionLimits(nil, nil, nil)
}

// reviewerGraph is a minimal single-agent graph granting only tool.workspace.read_file.
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
