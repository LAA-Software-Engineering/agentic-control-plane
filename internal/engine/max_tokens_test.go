package engine

import (
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/models"
	"github.com/Terfyn/terfyn/internal/spec"
)

// A max_tokens stop in the tool loop fails with the actionable message (naming the cap and the
// constraint to raise), not the opaque "stop reason ... is not end_turn or tool_use" (issue #514).
func TestRun_agentToolLoop_maxTokensStopIsActionable(t *testing.T) {
	graph := agentLoopGraph(t, spec.AgentSpec{
		Instructions: "Do work.",
		Tools:        []string{"helper"},
		Constraints:  &spec.AgentConstraints{MaxTokens: 5000},
	}, spec.PolicySpec{})
	mock := &models.MockClient{Script: []models.MockTurn{{Content: "partial", StopReason: models.StopReasonMaxTokens}}}
	got, _, err := runAgentLoop(t, graph, mock, nil)
	if err == nil {
		t.Fatalf("expected a max_tokens error, run status %q", got.Status)
	}
	if !strings.Contains(err.Error(), "max_tokens=5000") || !strings.Contains(err.Error(), "constraints { maxTokens") {
		t.Fatalf("error is not actionable: %v", err)
	}
	// The cap the agent set was actually sent to the provider.
	if reqs := mock.Requests(); len(reqs) == 0 || reqs[0].MaxTokens != 5000 {
		t.Fatalf("request MaxTokens = %v, want 5000", reqs)
	}
}

// The no-tools (single completion) path handles a max_tokens stop the same way, rather than feeding a
// truncated output object into schema validation/parse.
func TestRun_agentSingleTurn_maxTokensStopIsActionable(t *testing.T) {
	graph := agentLoopGraph(t, spec.AgentSpec{
		Instructions: "Summarize.",
		Constraints:  &spec.AgentConstraints{MaxTokens: 8000},
	}, spec.PolicySpec{})
	mock := &models.MockClient{Script: []models.MockTurn{{Content: `{"summary":"trunc`, StopReason: models.StopReasonMaxTokens}}}
	_, _, err := runAgentLoop(t, graph, mock, nil)
	if err == nil {
		t.Fatal("expected a max_tokens error on the single-completion path")
	}
	if !strings.Contains(err.Error(), "max_tokens=8000") {
		t.Fatalf("error is not actionable: %v", err)
	}
}

// When the agent sets no maxTokens, the default (not the old 4096) is sent to the provider.
func TestRun_agentToolLoop_defaultMaxTokensSent(t *testing.T) {
	graph := agentLoopGraph(t, spec.AgentSpec{Instructions: "Summarize."}, spec.PolicySpec{})
	mock := &models.MockClient{Content: `{"summary":"ok"}`}
	if _, _, err := runAgentLoop(t, graph, mock, nil); err != nil {
		t.Fatal(err)
	}
	reqs := mock.Requests()
	if len(reqs) == 0 || reqs[0].MaxTokens != spec.DefaultAgentMaxTokens {
		t.Fatalf("default MaxTokens = %v, want %d", reqs, spec.DefaultAgentMaxTokens)
	}
}
