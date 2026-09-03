package agentcli

import (
	"testing"

	"github.com/Terfyn/terfyn/internal/execir"
	"github.com/Terfyn/terfyn/internal/spec"
)

// resolveDrivenAgent gates on the executable, not a distinct-agent count (issue #367 review): a
// single-agent workflow wrapped in control flow, or a multi-step chain, drops orchestration when
// spawned once, so it must be refused — not silently accepted.
func TestResolveDrivenAgent_gatesOnExecutable(t *testing.T) {
	graph := reviewerGraph()
	graph.Agents["Other"] = &spec.AgentResource{Metadata: spec.Metadata{Name: "Other"}, Spec: spec.AgentSpec{Model: "mock/gpt-4"}}
	wf := &spec.WorkflowResource{Metadata: spec.Metadata{Name: "review"}}

	prog := func(nodes ...execir.Node) map[string]*execir.Program {
		return map[string]*execir.Program{"review": {Workflow: "review", Body: nodes}}
	}

	// Faithful: exactly one unconditional agent invocation (+ return).
	ar, err := resolveDrivenAgent(graph, wf, prog(&execir.InvokeAgent{Agent: "Reviewer"}, &execir.Return{}))
	if err != nil || ar == nil || ar.Metadata.Name != "Reviewer" {
		t.Fatalf("faithful single-agent workflow: ar=%v err=%v", ar, err)
	}

	// Refused shapes.
	cases := map[string]*execir.Program{
		"control flow (retry)":  {Body: []execir.Node{&execir.InvokeAgent{Agent: "Reviewer"}, &execir.Retry{}}},
		"control flow (branch)": {Body: []execir.Node{&execir.Branch{}}},
		"multi-agent":           {Body: []execir.Node{&execir.InvokeAgent{Agent: "Reviewer"}, &execir.InvokeAgent{Agent: "Other"}}},
		"multi-step chain":      {Body: []execir.Node{&execir.InvokeAgent{Agent: "Reviewer"}, &execir.InvokeTool{Uses: "tool.workspace.read_file"}}},
		"tool-only (no agent)":  {Body: []execir.Node{&execir.InvokeTool{Uses: "tool.workspace.read_file"}}},
		"subworkflow":           {Body: []execir.Node{&execir.InvokeWorkflow{Workflow: "other"}, &execir.InvokeAgent{Agent: "Reviewer"}}},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := resolveDrivenAgent(graph, wf, map[string]*execir.Program{"review": p}); err == nil {
				t.Fatalf("%s must be refused by the external runtime", name)
			}
		})
	}

	// No executable at all is refused (fail closed).
	if _, err := resolveDrivenAgent(graph, wf, nil); err == nil {
		t.Fatal("a workflow with no executable must be refused")
	}
}
