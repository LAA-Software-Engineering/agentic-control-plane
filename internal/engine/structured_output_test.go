package engine

import (
	"encoding/json"
	"testing"

	"github.com/Terfyn/terfyn/internal/models"
	"github.com/Terfyn/terfyn/internal/spec"
)

func agentWithConstraints(schemaRef string, c *spec.AgentConstraints) *spec.AgentResource {
	a := &spec.AgentResource{Metadata: spec.Metadata{Name: "a"}, Spec: spec.AgentSpec{Constraints: c}}
	if schemaRef != "" {
		a.Spec.Output = &spec.AgentIO{Schema: schemaRef}
	}
	return a
}

func TestAgentResponseFormat(t *testing.T) {
	e := &Executor{PinnedGraph: true, Schemas: map[string]string{"./out.json": strictInputSchema}}

	// Constraint unset → no structured output.
	if rf, err := e.agentResponseFormat(agentWithConstraints("./out.json", nil)); err != nil || rf != nil {
		t.Fatalf("unset constraint: rf=%v err=%v", rf, err)
	}
	if rf, err := e.agentResponseFormat(agentWithConstraints("./out.json", &spec.AgentConstraints{})); err != nil || rf != nil {
		t.Fatalf("false constraint: rf=%v err=%v", rf, err)
	}

	// Constraint set + output schema → response format carrying the schema content and agent name.
	rf, err := e.agentResponseFormat(agentWithConstraints("./out.json", &spec.AgentConstraints{RequireStructuredOutput: true}))
	if err != nil {
		t.Fatal(err)
	}
	if rf == nil {
		t.Fatal("expected response format")
	}
	if string(rf.Schema) != strictInputSchema {
		t.Fatalf("schema = %s", rf.Schema)
	}
	if rf.Name != "a" {
		t.Fatalf("name = %q, want a", rf.Name)
	}

	// Constraint set + no output schema → run error (not a silent skip).
	if _, err := e.agentResponseFormat(agentWithConstraints("", &spec.AgentConstraints{RequireStructuredOutput: true})); err == nil {
		t.Fatal("expected error when requireStructuredOutput has no output schema")
	}

	// Pinned resume whose snapshot did not capture the schema → gradual, structured output left off.
	missing := agentWithConstraints("./not-captured.json", &spec.AgentConstraints{RequireStructuredOutput: true})
	if rf, err := e.agentResponseFormat(missing); err != nil || rf != nil {
		t.Fatalf("uncaptured pinned schema: rf=%v err=%v", rf, err)
	}
}

func TestStructuredOutputName(t *testing.T) {
	cases := map[string]string{
		"reviewer":   "reviewer",
		"code-agent": "code-agent",
		"triager_v2": "triager_v2",
		"my agent!":  "my_agent",
		"__weird__":  "weird",
		"":           "output",
		"!!!":        "output",
	}
	for in, want := range cases {
		if got := structuredOutputName(in); got != want {
			t.Fatalf("structuredOutputName(%q) = %q, want %q", in, got, want)
		}
	}
}

// End-to-end, single completion (no tools): an agent whose constraints require structured output has
// its output schema sent to the provider as a response format on the finishAgentTurn path (issue #510).
func TestRun_agentLoop_wiresStructuredOutput(t *testing.T) {
	graph := agentLoopGraph(t, spec.AgentSpec{
		Instructions: "Summarize.",
		Constraints:  &spec.AgentConstraints{RequireStructuredOutput: true},
	}, spec.PolicySpec{})
	mock := &models.MockClient{Content: `{"summary":"ok"}`}
	got, _, err := runAgentLoop(t, graph, mock, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "succeeded" {
		t.Fatalf("status %q err=%q", got.Status, got.ErrorText)
	}
	reqs := mock.Requests()
	if len(reqs) != 1 {
		t.Fatalf("generates = %d, want 1", len(reqs))
	}
	assertResponseFormat(t, reqs)
}

// End-to-end, multi-turn tool loop: the response format must be set on EVERY Generate, not only the
// no-tools finishAgentTurn path — a tool_use turn and the final end_turn turn both carry it, matching
// the CHANGELOG's "applied on every turn of the agent tool loop" claim (issue #510). MockClient
// records the field but does not enforce it, so this pins the engine contract, not provider behavior.
func TestRun_agentToolLoop_wiresStructuredOutputEveryTurn(t *testing.T) {
	graph := agentLoopGraph(t, spec.AgentSpec{
		Instructions: "Use helper then summarize.",
		Tools:        []string{"helper"},
		Constraints:  &spec.AgentConstraints{RequireStructuredOutput: true},
	}, spec.PolicySpec{})
	mock := &models.MockClient{
		Script: []models.MockTurn{
			{ToolCalls: []models.ToolCall{{ID: "call_1", Name: "helper", Arguments: json.RawMessage(`{"q":"x"}`)}}},
			{Content: `{"summary":"used helper"}`},
		},
	}
	got, _, err := runAgentLoop(t, graph, mock, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "succeeded" {
		t.Fatalf("status %q err=%q", got.Status, got.ErrorText)
	}
	reqs := mock.Requests()
	if len(reqs) != 2 {
		t.Fatalf("generates = %d, want 2 (tool_use turn + final turn)", len(reqs))
	}
	// The first request is the tool-loop turn (advertises the helper tool); assert it too.
	if len(reqs[0].Tools) == 0 {
		t.Fatalf("first request expected to advertise tools (tool-loop turn), got none")
	}
	assertResponseFormat(t, reqs)
}

func assertResponseFormat(t *testing.T, reqs []models.GenerateRequest) {
	t.Helper()
	for i, r := range reqs {
		if r.ResponseFormat == nil {
			t.Fatalf("request %d missing ResponseFormat", i)
		}
		if r.ResponseFormat.Name != "reviewer" {
			t.Fatalf("request %d name = %q, want reviewer", i, r.ResponseFormat.Name)
		}
		if len(r.ResponseFormat.Schema) == 0 {
			t.Fatalf("request %d empty schema", i)
		}
	}
}

// Without the constraint, ordinary agent runs send no response format (unchanged behavior).
func TestRun_agentLoop_noStructuredOutputByDefault(t *testing.T) {
	graph := agentLoopGraph(t, spec.AgentSpec{Instructions: "Summarize."}, spec.PolicySpec{})
	mock := &models.MockClient{Content: `{"summary":"ok"}`}
	if _, _, err := runAgentLoop(t, graph, mock, nil); err != nil {
		t.Fatal(err)
	}
	for i, r := range mock.Requests() {
		if r.ResponseFormat != nil {
			t.Fatalf("request %d unexpectedly carried ResponseFormat", i)
		}
	}
}
