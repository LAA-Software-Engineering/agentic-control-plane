package mcpserver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Terfyn/terfyn/internal/audit"
	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
	"github.com/Terfyn/terfyn/internal/trace"
)

// A traced dispatch emits tool_selection + tool_execution into the run's hash-linked chain, with
// the same event types/actor as the internal loop, and the chain verifies.
func TestPolicyDispatcher_EmitsVerifiableToolTrace(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "trace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	start := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: "run-x", WorkflowName: "wf", Env: "dev", Status: "running", StartedAt: start,
		InputJSON: `{}`, TenantID: "t1", ThreadID: "th1", ActorID: "a1",
	}); err != nil {
		t.Fatal(err)
	}
	rec := trace.NewRecorder(st)

	g := dispatchGraph()
	d := NewPolicyDispatcher(policy.NewEvaluator(g, nil), &recordingExecutor{}, policy.RunContext{}).
		WithTrace(rec, "run-x")
	if _, err := d.Call(ctx, "tool.workspace.read_file", map[string]any{"path": "x"}); err != nil {
		t.Fatal(err)
	}

	events, err := st.ListTraceEventsByRunID(ctx, "run-x")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("want tool_selection + tool_execution, got %d events", len(events))
	}
	if events[0].Type != string(trace.EventToolSelection) || events[0].ActorType != string(trace.ActorAgent) {
		t.Fatalf("event[0] = %+v", events[0])
	}
	if events[1].Type != string(trace.EventToolExecution) || events[1].ActorType != string(trace.ActorAgent) {
		t.Fatalf("event[1] = %+v", events[1])
	}
	if err := audit.VerifyRunChainError("run-x", events); err != nil {
		t.Fatalf("emitted chain must verify: %v", err)
	}
}

// A policy-denied call records a system_error and skips tool_selection/tool_execution — the same
// fail-closed shape the internal loop emits, so the chain stays indistinguishable from a local run.
func TestPolicyDispatcher_TracesDeniedCall(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "trace2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	start := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: "run-y", WorkflowName: "wf", Env: "dev", Status: "running", StartedAt: start, InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	rec := trace.NewRecorder(st)

	g := dispatchGraph() // closed manifest: write_file is not granted
	d := NewPolicyDispatcher(policy.NewEvaluator(g, nil), &recordingExecutor{}, policy.RunContext{}).
		WithTrace(rec, "run-y")
	if _, err := d.Call(ctx, "tool.workspace.write_file", nil); err == nil {
		t.Fatal("expected a closed-world denial")
	}

	events, err := st.ListTraceEventsByRunID(ctx, "run-y")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != string(trace.EventSystemError) || events[0].ActorType != string(trace.ActorSystem) {
		t.Fatalf("denied call should record one system_error and skip the tool events, got %+v", events)
	}
	if err := audit.VerifyRunChainError("run-y", events); err != nil {
		t.Fatalf("chain must verify: %v", err)
	}
}
