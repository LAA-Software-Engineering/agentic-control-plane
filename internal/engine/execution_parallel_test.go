package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/audit"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

func fanInWorkflowGraph(t *testing.T) *spec.ProjectGraph {
	t.Helper()
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
			"fanin": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "fanin"},
				Spec: spec.WorkflowSpec{
					Policy: "default",
					Steps: []spec.WorkflowStep{
						{ID: "left", Uses: "tool.helper.echo", With: map[string]any{"which": "left"}},
						{ID: "right", Uses: "tool.helper.echo", With: map[string]any{"which": "right"}},
						{
							ID:    "join",
							Uses:  "tool.helper.echo",
							Needs: []string{"left", "right"},
							With: map[string]any{
								"which":   "join",
								"message": "${steps.left.output.echo} ${steps.right.output.echo}",
							},
						},
					},
					Output: &spec.WorkflowOutput{
						Value: map[string]any{"combined": "${steps.join.output.message}"},
					},
				},
			},
		},
	}
}

func startFanInRun(t *testing.T, st *sqlite.Store, runID string) (context.Context, time.Time, map[string]any) {
	t.Helper()
	ctx := context.Background()
	started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "fanin", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: `{}`, TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}
	return ctx, started, map[string]any{}
}

func whichOf(req tools.ToolCallRequest) string {
	w, _ := req.With["which"].(string)
	return w
}

// startBarrier is a two-party rendezvous: Wait returns true only if a peer also
// called Wait before timeout. A timed-out waiter leaves so a later sequential
// step cannot "complete" the barrier after the first has already returned.
type startBarrier struct {
	mu      sync.Mutex
	waiting int
	ready   chan struct{}
	once    sync.Once
}

func newStartBarrier() *startBarrier {
	return &startBarrier{ready: make(chan struct{})}
}

func (b *startBarrier) Wait(ctx context.Context, timeout time.Duration) bool {
	b.mu.Lock()
	b.waiting++
	if b.waiting >= 2 {
		b.once.Do(func() { close(b.ready) })
	}
	b.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-b.ready:
		return true
	case <-ctx.Done():
		b.depart()
		return false
	case <-timer.C:
		b.depart()
		return false
	}
}

func (b *startBarrier) depart() {
	b.mu.Lock()
	if b.waiting > 0 {
		b.waiting--
	}
	b.mu.Unlock()
}

func TestRun_parallelBranchesOverlapInTime(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "overlap.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := fanInWorkflowGraph(t)
	runID := "run-overlap"
	ctx, started, input := startFanInRun(t, st, runID)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	t.Cleanup(cancel)

	gate := newStartBarrier()
	var leftMet, rightMet atomic.Bool
	var leftDone, rightDone atomic.Bool

	ex := &Executor{
		Graph: graph, ProjectRoot: t.TempDir(),
		Tools: &tools.MockExecutor{Fn: func(ctx context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
			w := whichOf(req)
			out := map[string]any{"echo": w}
			switch w {
			case "left", "right":
				met := gate.Wait(ctx, 5*time.Second)
				if w == "left" {
					leftMet.Store(met)
					leftDone.Store(true)
				} else {
					rightMet.Store(met)
					rightDone.Store(true)
				}
				if !met {
					return tools.ToolCallResponse{}, fmt.Errorf("rendezvous timeout: peer did not enter %s concurrently", w)
				}
			case "join":
				if !leftDone.Load() || !rightDone.Load() {
					return tools.ToolCallResponse{}, fmt.Errorf("join started before both branches returned")
				}
				out["message"] = req.With["message"]
			}
			return tools.ToolCallResponse{Output: out}, nil
		}},
		Store: st, Trace: trace.NewRecorder(st),
	}
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "fanin", Env: "dev", StartedAt: started, Input: input,
	}); err != nil {
		t.Fatal(err)
	}
	if !leftMet.Load() || !rightMet.Load() {
		t.Fatal("independent steps did not rendezvous: both must observe the other has started")
	}

	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.OutputJSON, "left") || !strings.Contains(got.OutputJSON, "right") {
		t.Fatalf("fan-in output %s", got.OutputJSON)
	}
}

func TestRun_implicitSequentialDoesNotRendezvous(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "seq.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := demoWorkflowGraph(t)
	graph.Workflows["demo"].Spec.Steps = []spec.WorkflowStep{
		{ID: "a", Uses: "tool.helper.echo", With: map[string]any{"which": "a"}},
		{ID: "b", Uses: "tool.helper.echo", With: map[string]any{"which": "b"}},
	}
	graph.Workflows["demo"].Spec.Output = &spec.WorkflowOutput{Value: map[string]any{"ok": "1"}}
	runID := "run-seq-order"
	started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "demo", Env: "dev", Status: "running",
		StartedAt: started, InputJSON: `{"topic":"x"}`, TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}

	gate := newStartBarrier()
	var aMet, bMet atomic.Bool
	ex := &Executor{
		Graph: graph, ProjectRoot: testProjectRoot(t),
		Tools: &tools.MockExecutor{Fn: func(ctx context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
			w := whichOf(req)
			met := gate.Wait(ctx, 250*time.Millisecond)
			if w == "a" {
				aMet.Store(met)
			} else {
				bMet.Store(met)
			}
			return tools.ToolCallResponse{Output: map[string]any{"echo": w}}, nil
		}},
		Store: st, Trace: trace.NewRecorder(st),
	}
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "demo", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "x"},
	}); err != nil {
		t.Fatal(err)
	}
	if aMet.Load() || bMet.Load() {
		t.Fatalf("implicit sequential steps rendezvoused (a=%v b=%v); YAML order must not overlap", aMet.Load(), bMet.Load())
	}
}

func TestRun_resumePartialParallelGroup(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "part.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := fanInWorkflowGraph(t)
	runID := "run-partial"
	ctx, started, input := startFanInRun(t, st, runID)

	var resumed atomic.Bool
	var calls []string
	var mu sync.Mutex
	ex := &Executor{
		Graph: graph, ProjectRoot: t.TempDir(),
		Tools: &tools.MockExecutor{Fn: func(ctx context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
			w := whichOf(req)
			mu.Lock()
			calls = append(calls, w)
			mu.Unlock()
			switch w {
			case "left":
				return tools.ToolCallResponse{Output: map[string]any{"echo": "L"}}, nil
			case "right":
				if !resumed.Load() {
					<-ctx.Done()
					return tools.ToolCallResponse{}, ctx.Err()
				}
				return tools.ToolCallResponse{Output: map[string]any{"echo": "R"}}, nil
			default:
				msg, _ := req.With["message"].(string)
				return tools.ToolCallResponse{Output: map[string]any{"echo": "J", "message": msg}}, nil
			}
		}},
		Store: st, Trace: trace.NewRecorder(st),
	}
	err = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "fanin", Env: "dev", StartedAt: started, Input: input,
		InterruptAfterStepID: "left",
	})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("err = %v want ErrInterrupted", err)
	}
	cp, err := st.GetLatestCheckpoint(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	var payload checkpointPayload
	if err := json.Unmarshal([]byte(cp.ContextJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Completed) != 1 || payload.Completed[0] != "left" {
		t.Fatalf("completed = %v want [left]", payload.Completed)
	}
	if _, ok := payload.Steps["right"]; ok {
		t.Fatal("right must not be in checkpoint after cancel")
	}

	if err := st.UpdateRunStatus(ctx, runID, state.RunStatusRunning); err != nil {
		t.Fatal(err)
	}
	resumed.Store(true)
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "fanin", Env: "dev", StartedAt: started, Input: input, Resume: true,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.RunStatusSucceeded {
		t.Fatalf("status %q err=%q", got.Status, got.ErrorText)
	}
	if !strings.Contains(got.OutputJSON, "L") || !strings.Contains(got.OutputJSON, "R") {
		t.Fatalf("resume fan-in output %s", got.OutputJSON)
	}
	mu.Lock()
	leftCalls := 0
	for _, c := range calls {
		if c == "left" {
			leftCalls++
		}
	}
	mu.Unlock()
	if leftCalls != 1 {
		t.Fatalf("left replayed: calls=%v", calls)
	}
}

func TestRun_parallelLogicalOrderAndAuditChain(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "logic.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := fanInWorkflowGraph(t)
	runID := "run-logic"
	ctx, started, input := startFanInRun(t, st, runID)
	ex := &Executor{
		Graph: graph, ProjectRoot: t.TempDir(),
		Tools: &tools.MockExecutor{Fn: func(ctx context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
			w := whichOf(req)
			if w == "left" || w == "right" {
				time.Sleep(20 * time.Millisecond)
			}
			out := map[string]any{"echo": w}
			if w == "join" {
				out["message"] = req.With["message"]
			}
			return tools.ToolCallResponse{Output: out}, nil
		}},
		Store: st, Trace: trace.NewRecorder(st),
	}
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "fanin", Env: "dev", StartedAt: started, Input: input,
	}); err != nil {
		t.Fatal(err)
	}

	events, err := trace.NewReader(st).ListByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.VerifyRunChainError(runID, events); err != nil {
		t.Fatal(err)
	}

	type stamped struct {
		order int
		seq   int64
		step  string
		typ   string
	}
	var rows []stamped
	for _, ev := range events {
		var data map[string]any
		if err := json.Unmarshal([]byte(ev.DataJSON), &data); err != nil {
			t.Fatal(err)
		}
		ord, ok := data["logicalOrder"]
		if !ok {
			continue
		}
		var n int
		switch v := ord.(type) {
		case float64:
			n = int(v)
		case int:
			n = v
		default:
			t.Fatalf("logicalOrder type %T", ord)
		}
		rows = append(rows, stamped{order: n, seq: ev.Seq, step: ev.StepID, typ: ev.Type})
	}
	if len(rows) == 0 {
		t.Fatal("expected logicalOrder on step events")
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].order != rows[j].order {
			return rows[i].order < rows[j].order
		}
		return rows[i].seq < rows[j].seq
	})
	joinSeen := false
	for _, r := range rows {
		if r.step == "join" {
			joinSeen = true
			if r.order != 2 {
				t.Fatalf("join logicalOrder = %d want 2", r.order)
			}
		}
		if joinSeen && (r.step == "left" || r.step == "right") {
			t.Fatalf("logical replay put branch %q after join", r.step)
		}
		if r.step == "left" && r.order != 0 {
			t.Fatalf("left logicalOrder = %d want 0", r.order)
		}
		if r.step == "right" && r.order != 1 {
			t.Fatalf("right logicalOrder = %d want 1", r.order)
		}
	}
}

func TestRun_parallelCostNotDoubleCounted(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "cost.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := fanInWorkflowGraph(t)
	runID := "run-cost"
	ctx, started, input := startFanInRun(t, st, runID)
	ex := &Executor{
		Graph: graph, ProjectRoot: t.TempDir(),
		Tools: &tools.MockExecutor{Fn: func(ctx context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
			w := whichOf(req)
			out := map[string]any{"echo": w}
			if w == "join" {
				out["message"] = req.With["message"]
			}
			return tools.ToolCallResponse{Output: out, Meta: tools.ToolCallMeta{CostUSD: 0.02}}, nil
		}},
		Store: st, Trace: trace.NewRecorder(st),
	}
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "fanin", Env: "dev", StartedAt: started, Input: input,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	want := 0.06
	if got.TotalCostUSD < want-0.0001 || got.TotalCostUSD > want+0.0001 {
		t.Fatalf("totalCostUsd = %v want %v (no double-count)", got.TotalCostUSD, want)
	}
}

func TestRun_resumeKeepsSiblingThatFinishedDuringInterrupt(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "sib.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := fanInWorkflowGraph(t)
	runID := "run-sib"
	ctx, started, input := startFanInRun(t, st, runID)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	t.Cleanup(cancel)

	gate := newStartBarrier()
	var calls []string
	var mu sync.Mutex
	ex := &Executor{
		Graph: graph, ProjectRoot: t.TempDir(),
		Tools: &tools.MockExecutor{Fn: func(ctx context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
			w := whichOf(req)
			mu.Lock()
			calls = append(calls, w)
			mu.Unlock()
			if w == "left" || w == "right" {
				if !gate.Wait(ctx, 5*time.Second) {
					return tools.ToolCallResponse{}, fmt.Errorf("rendezvous timeout on %s", w)
				}
				return tools.ToolCallResponse{Output: map[string]any{"echo": w[:1]}}, nil
			}
			msg, _ := req.With["message"].(string)
			return tools.ToolCallResponse{Output: map[string]any{"echo": "J", "message": msg}}, nil
		}},
		Store: st, Trace: trace.NewRecorder(st),
	}
	err = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "fanin", Env: "dev", StartedAt: started, Input: input,
		InterruptAfterStepID: "left",
	})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("err = %v want ErrInterrupted", err)
	}
	cp, err := st.GetLatestCheckpoint(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	var payload checkpointPayload
	if err := json.Unmarshal([]byte(cp.ContextJSON), &payload); err != nil {
		t.Fatal(err)
	}
	gotCompleted := map[string]struct{}{}
	for _, id := range payload.Completed {
		gotCompleted[id] = struct{}{}
	}
	if _, ok := gotCompleted["left"]; !ok {
		t.Fatalf("completed = %v want left", payload.Completed)
	}
	if _, ok := gotCompleted["right"]; !ok {
		t.Fatalf("right finished during interrupt must be checkpointed, completed=%v", payload.Completed)
	}
	if _, ok := payload.Steps["right"]; !ok {
		t.Fatal("right output missing from interrupt checkpoint")
	}

	if err := st.UpdateRunStatus(ctx, runID, state.RunStatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "fanin", Env: "dev", StartedAt: started, Input: input, Resume: true,
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	rightCalls := 0
	for _, c := range calls {
		if c == "right" {
			rightCalls++
		}
	}
	mu.Unlock()
	if rightCalls != 1 {
		t.Fatalf("right replayed after interrupt: calls=%v", calls)
	}
}

func TestRun_parallelCostCapRejectsJointOverage(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "cap.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	graph := fanInWorkflowGraph(t)
	graph.Policies["default"].Spec.Execution = &spec.PolicyExecution{MaxTotalCostUsd: 1.00}
	runID := "run-cap"
	ctx, started, input := startFanInRun(t, st, runID)
	gate := newStartBarrier()
	ex := &Executor{
		Graph: graph, ProjectRoot: t.TempDir(),
		Tools: &tools.MockExecutor{Fn: func(ctx context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
			w := whichOf(req)
			cost := 0.0
			if w == "left" || w == "right" {
				if !gate.Wait(ctx, 5*time.Second) {
					return tools.ToolCallResponse{}, fmt.Errorf("rendezvous timeout on %s", w)
				}
				cost = 0.60
			}
			out := map[string]any{"echo": w}
			if w == "join" {
				out["message"] = req.With["message"]
			}
			return tools.ToolCallResponse{Output: out, Meta: tools.ToolCallMeta{CostUSD: cost}}, nil
		}},
		Store: st, Trace: trace.NewRecorder(st),
	}
	err = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "fanin", Env: "dev", StartedAt: started, Input: input,
	})
	if err == nil {
		t.Fatal("expected joint $0.60+$0.60 to fail a $1.00 cap")
	}
	if !strings.Contains(err.Error(), "exceeds ceiling") {
		t.Fatalf("want ceiling error, got %v", err)
	}
	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.RunStatusFailed {
		t.Fatalf("status %q want failed", got.Status)
	}
	rows, err := st.ListRunStepsByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	succeeded := 0
	for _, row := range rows {
		if (row.StepID == "left" || row.StepID == "right") && row.Status == "succeeded" {
			succeeded++
		}
	}
	if succeeded == 2 {
		t.Fatal("both concurrent $0.60 steps succeeded against a $1.00 cap")
	}
}
