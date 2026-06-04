package inspect

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
)

func seedInspectorDB(t *testing.T) (string, *sqlite.Store) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "inspect.db")
	st, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if err := st.UpsertAppliedResource(ctx, state.AppliedResource{
		Kind: spec.KindAgent, Name: "a1", Env: "local",
		SpecHash: "h1", NormalizedSpecJSON: `{"x":1}`, AppliedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.StartRun(ctx, state.Run{
		RunID: "run-1", WorkflowName: "demo", Env: "local", Status: state.RunStatusSucceeded,
		StartedAt: now, InputJSON: `{"in":true}`, TotalCostUSD: 0.1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertRunStep(ctx, state.RunStep{
		RunID: "run-1", StepID: "s1", Status: "ok", StartedAt: &now, CostUSD: 0.05,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTraceEvent(ctx, "run-1", now, "run.started", "", `{}`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTraceEvent(ctx, "run-1", now.Add(time.Second), "run.finished", "", `{"trace_id":"abc"}`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	ro, err := sqlite.OpenReadOnly(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	return path, ro
}

func TestServer_API_readOnly(t *testing.T) {
	path, st := seedInspectorDB(t)
	t.Cleanup(func() { _ = st.Close() })

	srv, err := NewServer(st, Config{
		StatePath:      path,
		Env:            "local",
		TraceUIBaseURL: "https://traces.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := RejectMutation(srv.Handler())
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	t.Run("POST rejected", func(t *testing.T) {
		res, err := http.Post(ts.URL+"/api/runs", "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("status=%d", res.StatusCode)
		}
	})

	t.Run("list runs", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/api/runs")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", res.StatusCode)
		}
		var body map[string]any
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		runs, ok := body["runs"].([]any)
		if !ok || len(runs) != 1 {
			t.Fatalf("runs=%v", body["runs"])
		}
	})

	t.Run("get run detail", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/api/runs/run-1")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["traceLink"] != "https://traces.example/abc" {
			t.Fatalf("traceLink=%v", body["traceLink"])
		}
		events, _ := body["events"].([]any)
		if len(events) != 2 {
			t.Fatalf("events=%v", body["events"])
		}
	})

	t.Run("state", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/api/state")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		resources, _ := body["resources"].([]any)
		if len(resources) != 1 {
			t.Fatalf("resources=%v", body["resources"])
		}
	})

	t.Run("unknown run", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/api/runs/missing")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("status=%d", res.StatusCode)
		}
	})

	t.Run("checkpoints require run", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/api/checkpoints")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("status=%d", res.StatusCode)
		}
	})

	t.Run("index html", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if !strings.Contains(string(b), "agentctl inspector") {
			t.Fatalf("body=%q", b)
		}
	})
}

func TestServer_readOnlyStoreCannotMutate(t *testing.T) {
	_, st := seedInspectorDB(t)
	t.Cleanup(func() { _ = st.Close() })

	err := st.StartRun(context.Background(), state.Run{
		RunID: "x", WorkflowName: "w", Env: "local", Status: "running",
		StartedAt: time.Now().UTC(), InputJSON: `{}`,
	})
	if err == nil {
		t.Fatal("expected write failure on read-only connection")
	}
}

func TestServer_handleGetRun_notFound(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "empty.db")
	rw, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = rw.Close()
	ro, err := sqlite.OpenReadOnly(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ro.Close() })

	srv, err := NewServer(ro, Config{StatePath: dbPath, Env: "local"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(RejectMutation(srv.Handler()))
	t.Cleanup(ts.Close)
	res, err := http.Get(ts.URL + "/api/runs/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("status=%d body=%s", res.StatusCode, b)
	}
}
