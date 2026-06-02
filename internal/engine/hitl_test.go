package engine

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

func hitlTestGraph() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"helper": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindTool,
				Metadata:   spec.Metadata{Name: "helper"},
				Spec: spec.ToolSpec{
					Type:   "native",
					Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(true)},
				},
			},
		},
		Policies: map[string]*spec.PolicyResource{
			"gate": {
				Spec: spec.PolicySpec{
					Approvals: &spec.PolicyApprovals{
						RequiredFor: []string{"tool.helper.echo"},
					},
					Hitl: &spec.HitlPolicy{
						InterruptOn: map[string]spec.HitlInterruptValue{
							"helper": {
								Enabled: true,
								Config: &spec.HitlInterruptConfig{
									AllowedDecisions: []spec.HitlDecisionKind{
										spec.HitlDecisionApprove,
										spec.HitlDecisionReject,
										spec.HitlDecisionEdit,
										spec.HitlDecisionSwitch,
									},
									AllowedEditArgs: []string{"topic"},
									SwitchMap: map[string][]string{
										"echo": {"identity"},
									},
								},
							},
						},
					},
				},
			},
		},
		Workflows: map[string]*spec.WorkflowResource{
			"hitl": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "hitl"},
				Spec: spec.WorkflowSpec{
					Policy: "gate",
					Steps: []spec.WorkflowStep{{
						ID: "fetch", Uses: "tool.helper.echo",
						With: map[string]any{"topic": "hello"},
					}},
					Output: &spec.WorkflowOutput{
						Value: map[string]any{"echo": "${steps.fetch.output.echo}"},
					},
				},
			},
		},
	}
}

func setupHitlExecutor(t *testing.T) (*Executor, *sqlite.Store, string, time.Time) {
	t.Helper()
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hitl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	graph := hitlTestGraph()
	runID := "run-hitl"
	started := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "hitl", Env: "local", Status: state.RunStatusRunning,
		StartedAt: started, InputJSON: `{}`, WorkflowSpecHash: "test-hash",
	}); err != nil {
		t.Fatal(err)
	}
	ex := &Executor{
		Graph: graph, Tools: tools.NewRegistry(graph), Store: st, Trace: trace.NewRecorder(st),
	}
	return ex, st, runID, started
}

func TestHitl_interruptThenApprove(t *testing.T) {
	ex, _, runID, started := setupHitlExecutor(t)
	ctx := context.Background()

	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "local", StartedAt: started, Input: map[string]any{},
	})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("first run: %v", err)
	}
	run, _ := ex.Store.GetRun(ctx, runID)
	if run.Status != state.RunStatusInterrupted {
		t.Fatalf("status = %q", run.Status)
	}

	err = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "local", StartedAt: started, Input: map[string]any{},
		Resume: true,
		Hitl: HitlRunOptions{
			Actor: "alice",
			Decision: &policy.HitlDecisionInput{
				Kind: spec.HitlDecisionApprove, Actor: "alice",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, _ = ex.Store.GetRun(ctx, runID)
	if run.Status != state.RunStatusSucceeded {
		t.Fatalf("status = %q err=%q", run.Status, run.ErrorText)
	}
	assertTraceContains(t, ex.Store, runID, trace.EventApprovalRequested, trace.EventApprovalResolved)
}

func TestHitl_autoApprove(t *testing.T) {
	ex, _, runID, started := setupHitlExecutor(t)
	ctx := context.Background()
	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "local", StartedAt: started, Input: map[string]any{},
		Hitl: HitlRunOptions{AutoApprove: true, Actor: "ci"},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, _ := ex.Store.GetRun(ctx, runID)
	if run.Status != state.RunStatusSucceeded {
		t.Fatalf("status = %q", run.Status)
	}
}

func TestHitl_rejectFailsStep(t *testing.T) {
	ex, _, runID, started := setupHitlExecutor(t)
	ctx := context.Background()
	_ = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "local", StartedAt: started, Input: map[string]any{},
	})
	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "local", StartedAt: started, Input: map[string]any{},
		Resume: true,
		Hitl: HitlRunOptions{
			Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionReject, Actor: "bob"},
		},
	})
	if err == nil {
		t.Fatal("expected rejection error")
	}
	if _, ok := policy.AsHitlRejected(err); !ok {
		t.Fatalf("expected HitlRejected, got %v", err)
	}
}

func TestHitl_editArgs(t *testing.T) {
	ex, _, runID, started := setupHitlExecutor(t)
	ctx := context.Background()
	_ = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "local", StartedAt: started, Input: map[string]any{},
	})
	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "local", StartedAt: started, Input: map[string]any{},
		Resume: true,
		Hitl: HitlRunOptions{
			Decision: &policy.HitlDecisionInput{
				Kind: spec.HitlDecisionEdit, Actor: "alice",
				EditedWith: map[string]any{"topic": "edited"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, _ := ex.Store.GetRun(ctx, runID)
	if run.Status != state.RunStatusSucceeded {
		t.Fatalf("status = %q", run.Status)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(run.OutputJSON), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run.OutputJSON, "edited") {
		t.Fatalf("output = %s", run.OutputJSON)
	}
}

func TestHitl_switchOperation(t *testing.T) {
	graph := hitlTestGraph()
	graph.Workflows["hitl"].Spec.Output = &spec.WorkflowOutput{
		Value: map[string]any{"value": "${steps.fetch.output.value}"},
	}
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hitl-switch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	runID := "run-hitl-switch"
	started := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "hitl", Env: "local", Status: state.RunStatusRunning,
		StartedAt: started, InputJSON: `{}`, WorkflowSpecHash: "test-hash",
	}); err != nil {
		t.Fatal(err)
	}
	ex := &Executor{
		Graph: graph, Tools: tools.NewRegistry(graph), Store: st, Trace: trace.NewRecorder(st),
	}
	_ = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "local", StartedAt: started, Input: map[string]any{},
	})
	err = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "local", StartedAt: started, Input: map[string]any{},
		Resume: true,
		Hitl: HitlRunOptions{
			Decision: &policy.HitlDecisionInput{
				Kind: spec.HitlDecisionSwitch, Actor: "alice", SwitchTarget: "identity",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, _ := ex.Store.GetRun(ctx, runID)
	if run.Status != state.RunStatusSucceeded {
		t.Fatalf("status = %q err=%q", run.Status, run.ErrorText)
	}
}

func TestHitl_editDeniedArgRejected(t *testing.T) {
	ex, _, runID, started := setupHitlExecutor(t)
	ctx := context.Background()
	_ = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "local", StartedAt: started, Input: map[string]any{},
	})
	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "local", StartedAt: started, Input: map[string]any{},
		Resume: true,
		Hitl: HitlRunOptions{
			Decision: &policy.HitlDecisionInput{
				Kind: spec.HitlDecisionEdit,
				EditedWith: map[string]any{
					"topic": "ok", "secret": "nope",
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected edit validation error")
	}
}

func assertTraceContains(t *testing.T, store state.RuntimeStore, runID string, types ...string) {
	t.Helper()
	events, err := trace.NewReader(store).ListByRunID(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, ev := range events {
		found[ev.Type] = true
	}
	for _, typ := range types {
		if !found[typ] {
			t.Fatalf("missing trace event %q in %d events", typ, len(events))
		}
	}
}
