package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/inspect"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
)

// TestIntegration_inspectWeb_API_afterRun seeds state via CLI run, then hits inspector HTTP API.
func TestIntegration_inspectWeb_API_afterRun(t *testing.T) {
	parent := t.TempDir()
	projName := "inspectweb"
	projDir := filepath.Join(parent, projName)
	db := filepath.Join(t.TempDir(), "inspect-web.db")

	out, err := runCLI(t, "init", projName, "--parent-dir", parent)
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	out, err = runCLI(t, "apply", "--project", projDir, "--state", db, "--auto-approve")
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	out, err = runCLI(t, "run", "workflow/hello", "--project", projDir, "--state", db, "--input", "topic=web-int")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	runID := extractRunID(out)
	if runID == "" {
		t.Fatalf("no run id in:\n%s", out)
	}

	st, err := sqlite.OpenReadOnly(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	srv, err := inspect.NewServer(st, inspect.Config{StatePath: db, Env: "local"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/api/runs/" + runID)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	if res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing security headers: %v", res.Header)
	}
	var body inspect.RunDetailResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Run.RunID != runID {
		t.Fatalf("run=%+v", body.Run)
	}
	if len(body.Events) == 0 {
		t.Fatal("expected trace events")
	}
}
