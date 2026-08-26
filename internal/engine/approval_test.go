package engine

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/audit"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

func approvalTestGraph() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"helper": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindTool,
				Metadata:   spec.Metadata{Name: "helper"},
				Spec: spec.ToolSpec{
					Type:   "native",
					Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(false)},
				},
			},
		},
		Policies: map[string]*spec.PolicyResource{
			"default": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindPolicy,
				Metadata:   spec.Metadata{Name: "default"},
				Spec:       spec.PolicySpec{},
			},
		},
		Workflows: map[string]*spec.WorkflowResource{
			"review": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "review"},
				Spec: spec.WorkflowSpec{
					Policy: "default",
					Steps: []spec.WorkflowStep{
						{
							ID:   "draft",
							Uses: "tool.helper.echo",
							With: map[string]any{"plan": "do-it"},
						},
						{
							ID:       "gate",
							Approval: &spec.WorkflowApprovalValue{Enabled: true},
							With:     map[string]any{"plan": "${steps.draft.output.echo.plan}"},
						},
						{
							ID:   "apply",
							Uses: "tool.helper.echo",
							With: map[string]any{"plan": "${steps.gate.output.plan}"},
						},
					},
					Output: &spec.WorkflowOutput{
						Value: map[string]any{"final": "${steps.apply.output.echo.plan}"},
					},
				},
			},
		},
	}
}

func setupApprovalExecutor(t *testing.T, graph *spec.ProjectGraph, runID string) (*Executor, *sqlite.Store, time.Time) {
	t.Helper()
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), runID+".db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if graph == nil {
		graph = approvalTestGraph()
	}
	started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: graphWorkflowName(graph), Env: "dev", Status: state.RunStatusRunning,
		StartedAt: started, InputJSON: `{}`, WorkflowSpecHash: "test-hash",
	}); err != nil {
		t.Fatal(err)
	}
	ex := &Executor{
		Graph: graph, ProjectRoot: t.TempDir(),
		Tools: tools.NewRegistry(graph), Store: st, Trace: trace.NewRecorder(st),
	}
	return ex, st, started
}

func graphWorkflowName(g *spec.ProjectGraph) string {
	for name := range g.Workflows {
		return name
	}
	return ""
}

func TestRun_approvalStep_interruptThenApprove(t *testing.T) {
	ex, st, started := setupApprovalExecutor(t, nil, "run-appr")
	ctx := context.Background()
	err := ex.Run(ctx, RunInput{
		RunID: "run-appr", WorkflowName: "review", Env: "dev", StartedAt: started, Input: map[string]any{},
	})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("first run: %v", err)
	}
	run, _ := ex.Store.GetRun(ctx, "run-appr")
	if run.Status != state.RunStatusInterrupted {
		t.Fatalf("status = %q", run.Status)
	}
	cp, err := st.GetLatestCheckpoint(ctx, "run-appr")
	if err != nil {
		t.Fatal(err)
	}
	if cp.Status != state.CheckpointStatusInterrupted || cp.StepID != "gate" {
		t.Fatalf("checkpoint %+v", cp)
	}
	var payload checkpointPayload
	if err := json.Unmarshal([]byte(cp.ContextJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PendingHitl == nil || payload.PendingHitl.StepID != "gate" || payload.PendingHitl.Kind != PendingHitlKindApproval {
		t.Fatalf("pending %+v", payload.PendingHitl)
	}

	if err := st.UpdateRunStatus(ctx, "run-appr", state.RunStatusRunning); err != nil {
		t.Fatal(err)
	}
	err = ex.Run(ctx, RunInput{
		RunID: "run-appr", WorkflowName: "review", Env: "dev", StartedAt: started, Input: map[string]any{},
		Resume: true,
		Hitl: HitlRunOptions{
			Actor:    "alice",
			Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, _ = ex.Store.GetRun(ctx, "run-appr")
	if run.Status != state.RunStatusSucceeded {
		t.Fatalf("status = %q err=%q", run.Status, run.ErrorText)
	}
	if !strings.Contains(run.OutputJSON, "do-it") {
		t.Fatalf("output = %s", run.OutputJSON)
	}
	assertTraceContains(t, ex.Store, "run-appr", trace.EventHitlRequestCreated, trace.EventHitlDecisionSubmitted, trace.EventHitlResolutionApplied)
}

func TestRun_approvalStep_rejectAbortsWorkflow(t *testing.T) {
	ex, st, started := setupApprovalExecutor(t, nil, "run-appr-rej")
	ctx := context.Background()
	_ = ex.Run(ctx, RunInput{
		RunID: "run-appr-rej", WorkflowName: "review", Env: "dev", StartedAt: started, Input: map[string]any{},
	})
	if err := st.UpdateRunStatus(ctx, "run-appr-rej", state.RunStatusRunning); err != nil {
		t.Fatal(err)
	}
	err := ex.Run(ctx, RunInput{
		RunID: "run-appr-rej", WorkflowName: "review", Env: "dev", StartedAt: started, Input: map[string]any{},
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
	run, _ := ex.Store.GetRun(ctx, "run-appr-rej")
	if run.Status != state.RunStatusFailed {
		t.Fatalf("status = %q (reject aborts the whole workflow)", run.Status)
	}
}

func TestRun_approvalStep_editResumesWithEditedPayload(t *testing.T) {
	ex, st, started := setupApprovalExecutor(t, nil, "run-appr-edit")
	ctx := context.Background()
	_ = ex.Run(ctx, RunInput{
		RunID: "run-appr-edit", WorkflowName: "review", Env: "dev", StartedAt: started, Input: map[string]any{},
	})
	if err := st.UpdateRunStatus(ctx, "run-appr-edit", state.RunStatusRunning); err != nil {
		t.Fatal(err)
	}
	err := ex.Run(ctx, RunInput{
		RunID: "run-appr-edit", WorkflowName: "review", Env: "dev", StartedAt: started, Input: map[string]any{},
		Resume: true,
		Hitl: HitlRunOptions{
			Decision: &policy.HitlDecisionInput{
				Kind:       spec.HitlDecisionEdit,
				Actor:      "alice",
				EditedWith: map[string]any{"plan": "edited"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, _ := ex.Store.GetRun(ctx, "run-appr-edit")
	if run.Status != state.RunStatusSucceeded {
		t.Fatalf("status = %q err=%q", run.Status, run.ErrorText)
	}
	if !strings.Contains(run.OutputJSON, "edited") {
		t.Fatalf("output = %s", run.OutputJSON)
	}
}

func TestRun_approvalStep_auditChainAcrossResume(t *testing.T) {
	ex, st, started := setupApprovalExecutor(t, nil, "run-appr-audit")
	ctx := context.Background()
	_ = ex.Run(ctx, RunInput{
		RunID: "run-appr-audit", WorkflowName: "review", Env: "dev", StartedAt: started, Input: map[string]any{},
	})
	if err := st.UpdateRunStatus(ctx, "run-appr-audit", state.RunStatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := ex.Run(ctx, RunInput{
		RunID: "run-appr-audit", WorkflowName: "review", Env: "dev", StartedAt: started, Input: map[string]any{},
		Resume: true,
		Hitl:   HitlRunOptions{Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"}},
	}); err != nil {
		t.Fatal(err)
	}
	events, err := trace.NewReader(st).ListByRunID(ctx, "run-appr-audit")
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.VerifyRunChainError("run-appr-audit", events); err != nil {
		t.Fatal(err)
	}
}

func parallelApprovalGraph() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"helper": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindTool,
				Metadata:   spec.Metadata{Name: "helper"},
				Spec: spec.ToolSpec{
					Type:   "native",
					Safety: &spec.ToolSafety{SideEffects: spec.BoolPtr(false)},
				},
			},
		},
		Policies: map[string]*spec.PolicyResource{
			"default": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindPolicy,
				Metadata:   spec.Metadata{Name: "default"},
				Spec:       spec.PolicySpec{},
			},
		},
		Workflows: map[string]*spec.WorkflowResource{
			"fan": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "fan"},
				Spec: spec.WorkflowSpec{
					Policy: "default",
					Steps: []spec.WorkflowStep{
						{
							ID:            "sib",
							Uses:          "tool.helper.echo",
							With:          map[string]any{"which": "sib"},
							NeedsDeclared: true,
						},
						{
							ID:            "gate",
							Approval:      &spec.WorkflowApprovalValue{Enabled: true},
							With:          map[string]any{"note": "pause"},
							NeedsDeclared: true,
						},
						{
							ID:    "join",
							Uses:  "tool.helper.echo",
							Needs: []string{"sib", "gate"},
							With: map[string]any{
								"from_sib":  "${steps.sib.output.echo.which}",
								"from_gate": "${steps.gate.output.note}",
							},
						},
					},
					Output: &spec.WorkflowOutput{
						Value: map[string]any{
							"sib":  "${steps.join.output.echo.from_sib}",
							"note": "${steps.join.output.echo.from_gate}",
						},
					},
				},
			},
		},
	}
}

func TestRun_approvalStep_parallelSuspendsOnlyItsBranch(t *testing.T) {
	graph := parallelApprovalGraph()
	ex, st, started := setupApprovalExecutor(t, graph, "run-appr-par")
	ctx := context.Background()
	err := ex.Run(ctx, RunInput{
		RunID: "run-appr-par", WorkflowName: "fan", Env: "dev", StartedAt: started, Input: map[string]any{},
	})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("first run: %v", err)
	}
	cp, err := st.GetLatestCheckpoint(ctx, "run-appr-par")
	if err != nil {
		t.Fatal(err)
	}
	var payload checkpointPayload
	if err := json.Unmarshal([]byte(cp.ContextJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PendingHitl == nil || payload.PendingHitl.StepID != "gate" || payload.PendingHitl.Kind != PendingHitlKindApproval {
		t.Fatalf("pending %+v", payload.PendingHitl)
	}
	steps, err := st.ListRunStepsByRunID(ctx, "run-appr-par")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if s.StepID == "sib" && s.Status == "failed" {
			t.Fatalf("sibling must not fail when the approval branch suspends: %+v", s)
		}
		if s.StepID == "join" && s.Status == "succeeded" {
			t.Fatal("join must not run before gate is approved")
		}
	}

	if err := st.UpdateRunStatus(ctx, "run-appr-par", state.RunStatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := ex.Run(ctx, RunInput{
		RunID: "run-appr-par", WorkflowName: "fan", Env: "dev", StartedAt: started, Input: map[string]any{},
		Resume: true,
		Hitl:   HitlRunOptions{Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"}},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetRun(ctx, "run-appr-par")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.RunStatusSucceeded {
		t.Fatalf("status %q err=%q", got.Status, got.ErrorText)
	}
	if !strings.Contains(got.OutputJSON, "sib") || !strings.Contains(got.OutputJSON, "pause") {
		t.Fatalf("output %s", got.OutputJSON)
	}
	events, err := trace.NewReader(st).ListByRunID(ctx, "run-appr-par")
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.VerifyRunChainError("run-appr-par", events); err != nil {
		t.Fatal(err)
	}
}
