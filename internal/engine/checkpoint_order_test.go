package engine

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

// TestRun_checkpointSurvivesMissingStepRow verifies resume works when the checkpoint
// was persisted before the run_steps succeeded row (crash window in PR #127 review).
func TestRun_checkpointSurvivesMissingStepRow(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "order.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := testProjectRoot(t)
	graph := demoWorkflowGraph(t)
	runID := "order-run"
	started := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	inJSON := `{"topic":"order"}`
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "demo", Env: "dev", Status: state.RunStatusRunning,
		StartedAt: started, InputJSON: inJSON, TotalCostUSD: 0,
	}); err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(inJSON), &input); err != nil {
		t.Fatal(err)
	}

	ex := &Executor{
		Graph: graph, ProjectRoot: root,
		Tools: tools.NewRegistry(graph), Models: models.NewRegistry(graph),
		Store: st, Trace: trace.NewRecorder(st),
		Now: func() time.Time { return started },
	}
	idx := 0
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "demo", Env: "dev", StartedAt: started, Input: input,
		InterruptAfterStepIndex: &idx,
	}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("interrupt: %v", err)
	}

	cp, err := st.GetLatestCheckpoint(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if cp.StepID != "fetch" {
		t.Fatalf("checkpoint step %q", cp.StepID)
	}

	// Simulate crash after checkpoint but before/during step row commit: remove succeeded row.
	if err := st.UpsertRunStep(ctx, state.RunStep{
		RunID: runID, StepID: "fetch", Status: "running",
		StartedAt: &started, InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}

	if err := st.UpdateRunStatus(ctx, runID, state.RunStatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "demo", Env: "dev", StartedAt: started, Input: input,
		Resume: true,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.RunStatusSucceeded {
		t.Fatalf("status %q", got.Status)
	}
}
