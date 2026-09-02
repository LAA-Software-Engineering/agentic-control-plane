package agentcli

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/audit"
	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/runtime/mcpserver"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
	"github.com/Terfyn/terfyn/internal/tools"
	"github.com/Terfyn/terfyn/internal/trace"
)

func newRunStore(t *testing.T, runID string) (*sqlite.Store, *trace.Recorder) {
	t.Helper()
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "run.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "wf", Env: "dev", Status: "running",
		StartedAt: time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC), InputJSON: `{}`,
		TenantID: "tenant-9", ThreadID: "thread-9", ActorID: "actor-9",
	}); err != nil {
		t.Fatal(err)
	}
	return st, trace.NewRecorder(st)
}

func TestEmitSessionTurns(t *testing.T) {
	ctx := context.Background()
	st, rec := newRunStore(t, "run-turns")
	session := Session{
		Model:      "claude-opus",
		CostUSD:    0.42,
		Turns:      []Turn{{Text: "thinking"}, {Text: "done"}},
		StopReason: StopSuccess,
	}
	if err := EmitSessionTurns(ctx, rec, "run-turns", "Coder", session); err != nil {
		t.Fatal(err)
	}
	events, err := st.ListTraceEventsByRunID(ctx, "run-turns")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("want one llm_completion per turn, got %d", len(events))
	}
	for _, e := range events {
		if e.Type != string(trace.EventLLMCompletion) || e.ActorType != string(trace.ActorAgent) {
			t.Fatalf("event = %+v", e)
		}
	}
}

func TestEmitSessionTurns_NilRecorderNoop(t *testing.T) {
	if err := EmitSessionTurns(context.Background(), nil, "r", "a", Session{Turns: []Turn{{}}}); err != nil {
		t.Fatalf("nil recorder must be a no-op, got %v", err)
	}
}

func TestEmitLimitHit(t *testing.T) {
	ctx := context.Background()
	st, rec := newRunStore(t, "run-limit")
	// A max_cost denial like EnforceBudget returns.
	eval := policy.NewEvaluator(&spec.ProjectGraph{}, &spec.PolicySpec{
		Execution: &spec.PolicyExecution{MaxTotalCostUsd: 1.0},
	})
	err := eval.CheckRun(ctx, policy.RunContext{AccumulatedCostUSD: 2.0})
	if err == nil {
		t.Fatal("expected a budget denial to emit")
	}
	if e := EmitLimitHit(ctx, rec, "run-limit", "budget", err); e != nil {
		t.Fatal(e)
	}
	events, lerr := st.ListTraceEventsByRunID(ctx, "run-limit")
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(events) != 1 || events[0].Type != string(trace.EventLimitHit) || events[0].ActorType != string(trace.ActorSystem) {
		t.Fatalf("want one limit_hit (system), got %+v", events)
	}
}

func TestEmitLimitHit_NonDenialIgnored(t *testing.T) {
	ctx := context.Background()
	st, rec := newRunStore(t, "run-noop")
	if err := EmitLimitHit(ctx, rec, "run-noop", "s", context.Canceled); err != nil {
		t.Fatal(err)
	}
	events, _ := st.ListTraceEventsByRunID(ctx, "run-noop")
	if len(events) != 0 {
		t.Fatalf("a non-denial must not emit a limit_hit, got %+v", events)
	}
}

// "Done when": a completed external-runtime run produces a tamper-evident chain indistinguishable
// in structure from a local run — run_started, the inner tool calls (via the mcpserver dispatcher),
// the agent turns, run_finished — and it re-walks cleanly under audit verify, with attribution
// preserved on the run.
func TestExternalRun_ProducesVerifiableChain(t *testing.T) {
	ctx := context.Background()
	const runID = "run-e2e"
	st, rec := newRunStore(t, runID)

	// run_started (as the runtime harness would emit).
	if _, err := rec.Append(ctx, runID, "", trace.EventRunStarted, trace.ActorSystem, nil); err != nil {
		t.Fatal(err)
	}

	// Inner tool call through the grant-compiled dispatcher (emits tool_selection + tool_execution).
	g := &spec.ProjectGraph{Tools: map[string]*spec.ToolResource{
		"workspace": tracedToolWithOps("workspace", "read_file"),
	}}
	disp := mcpserver.NewPolicyDispatcher(policy.NewEvaluator(g, nil), &echoExec{}, policy.RunContext{}).
		WithTrace(rec, runID)
	if _, err := disp.Call(ctx, "tool.workspace.read_file", map[string]any{"path": "main.go"}); err != nil {
		t.Fatal(err)
	}

	// Agent turns + run_finished.
	session := Session{Model: "claude-opus", CostUSD: 0.1, Turns: []Turn{{Text: "ok"}}, StopReason: StopSuccess}
	if err := EmitSessionTurns(ctx, rec, runID, "Coder", session); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Append(ctx, runID, "", trace.EventRunFinished, trace.ActorSystem, nil); err != nil {
		t.Fatal(err)
	}

	events, err := st.ListTraceEventsByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	wantSeq := []string{
		string(trace.EventRunStarted),
		string(trace.EventToolSelection),
		string(trace.EventToolExecution),
		string(trace.EventLLMCompletion),
		string(trace.EventRunFinished),
	}
	if len(events) != len(wantSeq) {
		t.Fatalf("event count = %d, want %d: %+v", len(events), len(wantSeq), events)
	}
	for i, want := range wantSeq {
		if events[i].Type != want {
			t.Fatalf("event[%d] = %q, want %q", i, events[i].Type, want)
		}
	}
	// The chain re-walks cleanly — tamper-evident, indistinguishable in structure from a local run.
	if err := audit.VerifyRunChainError(runID, events); err != nil {
		t.Fatalf("external-run chain must verify: %v", err)
	}
	// Attribution is preserved on the run the chain belongs to.
	run, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.TenantID != "tenant-9" || run.ThreadID != "thread-9" || run.ActorID != "actor-9" {
		t.Fatalf("attribution not preserved: %+v", run)
	}
}

func tracedToolWithOps(name string, ops ...string) *spec.ToolResource {
	m := make(map[string]spec.ToolOperation, len(ops))
	for _, op := range ops {
		m[op] = spec.ToolOperation{Effects: []string{"example.effect"}}
	}
	tr := &spec.ToolResource{
		APIVersion: spec.APIVersionV0, Kind: spec.KindTool,
		Metadata: spec.Metadata{Name: name},
		Spec:     spec.ToolSpec{Type: "mock", Operations: m},
	}
	tr.Spec.Safety = &spec.ToolSafety{
		Trusted: spec.BoolPtr(true), SideEffects: spec.BoolPtr(false), RequiresApproval: spec.BoolPtr(false),
	}
	return tr
}

type echoExec struct{}

func (echoExec) Call(_ context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
	return tools.ToolCallResponse{Output: map[string]any{"echoed": req.Uses}}, nil
}
