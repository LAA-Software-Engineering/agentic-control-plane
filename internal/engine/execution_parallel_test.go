package engine

import (
	"context"
	"encoding/json"
	"errors"
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

	type window struct{ start, end time.Time }
	var mu sync.Mutex
	times := map[string]window{}

	ex := &Executor{
		Graph: graph, ProjectRoot: t.TempDir(),
		Tools: &tools.MockExecutor{Fn: func(ctx context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
			w := whichOf(req)
			begin := time.Now()
			if w == "left" || w == "right" {
				time.Sleep(120 * time.Millisecond)
			}
			end := time.Now()
			mu.Lock()
			times[w] = window{start: begin, end: end}
			mu.Unlock()
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

	mu.Lock()
	left, okL := times["left"]
	right, okR := times["right"]
	join, okJ := times["join"]
	mu.Unlock()
	if !okL || !okR || !okJ {
		t.Fatalf("missing windows: %+v", times)
	}
	if !left.start.Before(left.end) || !right.start.Before(right.end) {
		t.Fatal("branch windows must be non-zero")
	}
	overlapped := left.start.Before(right.end) && right.start.Before(left.end)
	if !overlapped {
		t.Fatalf("left [%s,%s] and right [%s,%s] did not overlap", left.start, left.end, right.start, right.end)
	}
	if join.start.Before(left.end) || join.start.Before(right.end) {
		t.Fatalf("join started before both branches finished: join=%s left.end=%s right.end=%s", join.start, left.end, right.end)
	}

	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.OutputJSON, "left") || !strings.Contains(got.OutputJSON, "right") {
		t.Fatalf("fan-in output %s", got.OutputJSON)
	}
}

func TestRun_implicitSequentialDoesNotOverlap(t *testing.T) {
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

	type window struct{ start, end time.Time }
	var mu sync.Mutex
	times := map[string]window{}
	ex := &Executor{
		Graph: graph, ProjectRoot: testProjectRoot(t),
		Tools: &tools.MockExecutor{Fn: func(ctx context.Context, req tools.ToolCallRequest) (tools.ToolCallResponse, error) {
			w := whichOf(req)
			begin := time.Now()
			time.Sleep(50 * time.Millisecond)
			end := time.Now()
			mu.Lock()
			times[w] = window{begin, end}
			mu.Unlock()
			return tools.ToolCallResponse{Output: map[string]any{"echo": w}}, nil
		}},
		Store: st, Trace: trace.NewRecorder(st),
	}
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "demo", Env: "dev", StartedAt: started, Input: map[string]any{"topic": "x"},
	}); err != nil {
		t.Fatal(err)
	}
	a, b := times["a"], times["b"]
	if a.start.Before(b.end) && b.start.Before(a.end) {
		t.Fatalf("implicit sequential steps overlapped: a=[%s,%s] b=[%s,%s]", a.start, a.end, b.start, b.end)
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
