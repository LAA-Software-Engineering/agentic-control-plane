package policy

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

func testGraphWithTools(names ...string) *spec.ProjectGraph {
	tools := make(map[string]*spec.ToolResource)
	for _, n := range names {
		tools[n] = &spec.ToolResource{
			APIVersion: spec.APIVersionV0,
			Kind:       spec.KindTool,
			Metadata:   spec.Metadata{Name: n},
			Spec:       spec.ToolSpec{Type: "mock"},
		}
	}
	return &spec.ProjectGraph{Tools: tools}
}

func TestCheckToolCall_forbidUnknownTools_unknownToolDenied(t *testing.T) {
	g := testGraphWithTools("github")
	pol := &spec.PolicySpec{
		Tools: &spec.PolicyTools{ForbidUnknownTools: true},
	}
	ev := NewEvaluator(g, pol)
	err := ev.CheckToolCall(context.Background(), ToolCallContext{
		Run:    RunContext{},
		StepID: "s1",
		Uses:   "tool.slack.message.send",
	})
	if err == nil {
		t.Fatal("expected denial")
	}
	d, ok := AsDenied(err)
	if !ok || d.Reason != ReasonUnknownTool {
		t.Fatalf("got %v", err)
	}
	if d.Uses != "tool.slack.message.send" {
		t.Fatalf("uses %q", d.Uses)
	}
}

func TestCheckToolCall_forbidUnknownTools_knownToolOK(t *testing.T) {
	g := testGraphWithTools("slack")
	g.Tools["slack"].Spec.Safety = &spec.ToolSafety{SideEffects: spec.BoolPtr(false)}
	pol := &spec.PolicySpec{
		Tools: &spec.PolicyTools{ForbidUnknownTools: true},
	}
	ev := NewEvaluator(g, pol)
	err := ev.CheckToolCall(context.Background(), ToolCallContext{
		Run:    RunContext{},
		StepID: "s1",
		Uses:   "tool.slack.message.send",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckToolCall_approvalRequired_withoutApprove_policyDeniedTraceAndError(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "policy_trace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	started := time.Date(2026, 4, 11, 10, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID:        "run-policy",
		WorkflowName: "wf",
		Env:          "dev",
		Status:       "running",
		StartedAt:    started,
		InputJSON:    `{}`,
		TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}

	g := testGraphWithTools("github")
	pol := &spec.PolicySpec{
		Tools: &spec.PolicyTools{ForbidUnknownTools: true},
		Approvals: &spec.PolicyApprovals{
			RequiredFor: []string{"tool.github.pull_request.merge"},
		},
	}
	ev := NewEvaluator(g, pol)
	call := ToolCallContext{
		Run:    RunContext{ApprovedActions: nil},
		StepID: "merge-step",
		Uses:   "tool.github.pull_request.merge",
	}
	err = ev.CheckToolCall(ctx, call)
	if err == nil {
		t.Fatal("expected denial")
	}
	d, ok := AsDenied(err)
	if !ok || d.Reason != ReasonApprovalRequired {
		t.Fatalf("got %v", err)
	}

	data := d.TraceData()
	rec := trace.NewRecorder(st)
	seq, err := rec.Append(ctx, "run-policy", "merge-step", trace.EventSystemError, trace.ActorSystem, data)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 1 {
		t.Fatalf("seq %d", seq)
	}

	rd := trace.NewReader(st)
	events, err := rd.ListByRunID(ctx, "run-policy")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != string(trace.EventSystemError) {
		t.Fatalf("events %+v", events)
	}
	if events[0].DataJSON == "" {
		t.Fatal("empty data")
	}
}

func TestCheckToolCall_approvalRequired_withApproveOK(t *testing.T) {
	g := testGraphWithTools("github")
	pol := &spec.PolicySpec{
		Approvals: &spec.PolicyApprovals{
			RequiredFor: []string{"tool.github.pull_request.merge"},
		},
	}
	ev := NewEvaluator(g, pol)
	err := ev.CheckToolCall(context.Background(), ToolCallContext{
		Run:  RunContext{ApprovedActions: []string{"tool.github.pull_request.merge"}},
		Uses: "tool.github.pull_request.merge",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckRun_maxWallClock(t *testing.T) {
	pol := &spec.PolicySpec{
		Execution: &spec.PolicyExecution{MaxWallClockSeconds: 60},
	}
	ev := NewEvaluator(nil, pol)
	err := ev.CheckRun(context.Background(), RunContext{Elapsed: 61 * time.Second})
	if err == nil {
		t.Fatal("expected denial")
	}
	d, _ := AsDenied(err)
	if d.Reason != ReasonMaxWallClock {
		t.Fatalf("%+v", d)
	}
}

func TestCheckRun_maxCost(t *testing.T) {
	pol := &spec.PolicySpec{
		Execution: &spec.PolicyExecution{MaxTotalCostUsd: 1.0},
	}
	ev := NewEvaluator(nil, pol)
	err := ev.CheckRun(context.Background(), RunContext{AccumulatedCostUSD: 1.01})
	if err == nil {
		t.Fatal("expected denial")
	}
	d, _ := AsDenied(err)
	if d.Reason != ReasonMaxCost {
		t.Fatalf("%+v", d)
	}
}

func TestCheckStep_requireStructuredOutput(t *testing.T) {
	pol := &spec.PolicySpec{
		Execution: &spec.PolicyExecution{RequireStructuredOutput: true},
	}
	ev := NewEvaluator(nil, pol)
	err := ev.CheckStep(context.Background(), StepContext{StepID: "x", OutputIsStructured: false})
	if err == nil {
		t.Fatal("expected denial")
	}
	d, _ := AsDenied(err)
	if d.Reason != ReasonStructuredOutput {
		t.Fatalf("%+v", d)
	}
}

func TestEngine_Evaluator_resolvesNamedPolicy(t *testing.T) {
	g := &spec.ProjectGraph{
		Policies: map[string]*spec.PolicyResource{
			"strict": {
				Metadata: spec.Metadata{Name: "strict"},
				Spec: spec.PolicySpec{
					Execution: &spec.PolicyExecution{MaxWallClockSeconds: 10},
				},
			},
		},
	}
	eng := NewEngine(g)
	ev := eng.Evaluator("strict")
	err := ev.CheckRun(context.Background(), RunContext{Elapsed: 30 * time.Second})
	if err == nil {
		t.Fatal("expected denial")
	}
}
