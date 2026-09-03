package claudecode

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Terfyn/terfyn/internal/config"
	"github.com/Terfyn/terfyn/internal/runtime"
	"github.com/Terfyn/terfyn/internal/runtime/agentcli"
	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
)

func TestRegistered(t *testing.T) {
	if !runtime.IsKnown(Name) {
		t.Fatalf("%q should be a known runtime", Name)
	}
	factory, err := runtime.Lookup(Name)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", Name, err)
	}
	if _, err := factory(runtime.Deps{}); err != nil {
		t.Fatalf("factory: %v", err)
	}
}

func TestResumeStillPending(t *testing.T) {
	r, err := NewFromDeps(runtime.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resume(context.Background(), nil, runtime.ResumeOptions{}); !errors.Is(err, agentcli.ErrResumePending) {
		t.Fatalf("Resume should be pending (#367 follow-up), got %v", err)
	}
	if h := r.Health(context.Background()); h.State != runtime.HealthOK {
		t.Fatalf("wired runtime Health should be OK, got %q", h.State)
	}
}

// TestInvoke_endToEnd drives the flagship single-agent workflow through the Claude driver + the
// shared agentcli adapter with a fake process, exercising the full run lifecycle: resolve the driven
// agent, create the run row + trace, run, and FinishRun.
func TestInvoke_endToEnd(t *testing.T) {
	ctx := context.Background()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "examples", "external-runtime-reviewer"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Resolve(config.ResolveOptions{ProjectRoot: root, Env: ""})
	if err != nil {
		t.Fatalf("resolve flagship project: %v", err)
	}
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "invoke.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	rt := agentcli.NewRuntimeAdapter(Name, ClaudeCodeRuntime{Run: fakeRunner(successStream, nil, nil)}, runtime.Deps{Store: st})
	res, err := rt.Invoke(ctx, cfg, runtime.InvokeOptions{
		WorkflowName: "review",
		InputJSON:    []byte(`{"change":"add a null check"}`),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if res.RunID == "" {
		t.Fatal("no run id")
	}

	run, err := st.GetRun(ctx, res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != state.RunStatusSucceeded {
		t.Fatalf("run status = %q, want succeeded", run.Status)
	}

	events, err := st.ListTraceEventsByRunID(ctx, res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 2 || events[0].Type != "run_started" || events[len(events)-1].Type != "run_finished" {
		t.Fatalf("run should be bracketed by run_started..run_finished, got %+v", events)
	}
}

// TestInvoke_passesPolicyBudgetToArgv is the regression for issue #389: the governing policy's
// execution.maxTotalCostUsd must reach the spawned `claude` as --max-budget-usd (the harness belt),
// not only be discovered after the session ends. The flagship reviewer policy sets maxTotalCostUsd: 5;
// with the fix the argv carries "--max-budget-usd 5", where before Invoke fed MapLimits a nil policy
// and the flag was always omitted.
func TestInvoke_passesPolicyBudgetToArgv(t *testing.T) {
	ctx := context.Background()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "examples", "external-runtime-reviewer"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Resolve(config.ResolveOptions{ProjectRoot: root, Env: ""})
	if err != nil {
		t.Fatalf("resolve flagship project: %v", err)
	}
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "budget.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	var argv []string
	rt := agentcli.NewRuntimeAdapter(Name, ClaudeCodeRuntime{Run: fakeRunner(successStream, nil, &argv)}, runtime.Deps{Store: st})
	if _, err := rt.Invoke(ctx, cfg, runtime.InvokeOptions{
		WorkflowName: "review",
		InputJSON:    []byte(`{"change":"add a null check"}`),
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !containsPair(argv, "--max-budget-usd", "5") {
		t.Fatalf("argv must carry the policy budget as --max-budget-usd 5, got %v", argv)
	}
}

// A workflow whose name is unknown is a clear error, not a silent no-op.
func TestInvoke_unknownWorkflow(t *testing.T) {
	ctx := context.Background()
	root, _ := filepath.Abs(filepath.Join("..", "..", "..", "examples", "external-runtime-reviewer"))
	cfg, err := config.Resolve(config.ResolveOptions{ProjectRoot: root, Env: ""})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := sqlite.Open(ctx, filepath.Join(t.TempDir(), "u.db"))
	t.Cleanup(func() { _ = st.Close() })
	rt := agentcli.NewRuntimeAdapter(Name, ClaudeCodeRuntime{Run: fakeRunner(successStream, nil, nil)}, runtime.Deps{Store: st})
	if _, err := rt.Invoke(ctx, cfg, runtime.InvokeOptions{WorkflowName: "nope"}); err == nil {
		t.Fatal("unknown workflow must error")
	}
}
