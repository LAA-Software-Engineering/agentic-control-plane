package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/inspect"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/statejson"
)

// TestLogsInspectContract_eventsJSON verifies agentctl logs -o json events match inspector /api/runs/{id}.
func TestLogsInspectContract_eventsJSON(t *testing.T) {
	db := filepath.Join(t.TempDir(), "contract.db")
	root := runProjRoot(t)

	ResetGlobalsForTest()
	runCmd := NewRootCmd()
	runCmd.SetOut(io.Discard)
	runCmd.SetErr(io.Discard)
	runCmd.SetArgs([]string{
		"run", "workflow/demo",
		"--project", root,
		"--state", db,
		"--input", "topic=contract-test",
	})
	var runOut bytes.Buffer
	runCmd.SetOut(&runOut)
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	runID := extractRunID(runOut.String())
	if runID == "" {
		t.Fatal("no run id")
	}

	ResetGlobalsForTest()
	var logsOut bytes.Buffer
	logsCmd := NewRootCmd()
	logsCmd.SetOut(&logsOut)
	logsCmd.SetErr(&logsOut)
	logsCmd.SetArgs([]string{"logs", "--run", runID, "--project", root, "--state", db, "-o", "json"})
	if err := logsCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var logsPayload statejson.RunEventsPayload
	if err := json.Unmarshal(logsOut.Bytes(), &logsPayload); err != nil {
		t.Fatalf("logs json: %v\n%s", err, logsOut.String())
	}

	ctx := context.Background()
	st, err := sqlite.OpenReadOnly(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	srv, err := inspect.NewServer(st, inspect.Config{StatePath: db, Env: "staging"})
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
	var api inspect.RunDetailResponse
	if err := json.NewDecoder(res.Body).Decode(&api); err != nil {
		t.Fatal(err)
	}

	logsEvents, err := json.Marshal(logsPayload.Events)
	if err != nil {
		t.Fatal(err)
	}
	apiEvents, err := json.Marshal(api.Events)
	if err != nil {
		t.Fatal(err)
	}
	if string(logsEvents) != string(apiEvents) {
		t.Fatalf("events mismatch:\nlogs %s\napi %s", logsEvents, apiEvents)
	}
}
