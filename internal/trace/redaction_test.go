package trace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
)

func TestPrepareEventData_redactsNestedKeys(t *testing.T) {
	data := map[string]any{
		"headers": map[string]any{
			"Authorization": "Bearer secret-token",
		},
		"api_key": "sk-live",
	}
	out, err := PrepareEventData(data, nil, DefaultRedactionOptions())
	if err != nil {
		t.Fatal(err)
	}
	headers, _ := out["headers"].(map[string]any)
	if headers["Authorization"] != RedactedPlaceholder {
		t.Fatalf("Authorization=%v", headers["Authorization"])
	}
	if out["api_key"] != RedactedPlaceholder {
		t.Fatalf("api_key=%v", out["api_key"])
	}
}

func TestPrepareEventData_maxDepth(t *testing.T) {
	nested := map[string]any{}
	cur := nested
	for i := 0; i < 80; i++ {
		next := map[string]any{}
		cur["child"] = next
		cur = next
	}
	opts := DefaultRedactionOptions()
	opts.MaxDepth = 5
	out, err := PrepareEventData(nested, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), "max depth 5 exceeded") {
		t.Fatalf("depth marker missing: %s", b)
	}
}

func TestPrepareEventData_truncatesPayload(t *testing.T) {
	data := map[string]any{"blob": strings.Repeat("x", 200)}
	opts := DefaultRedactionOptions()
	opts.MaxPayloadBytes = 40
	out, err := PrepareEventData(data, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if out["payload_truncated"] != true {
		t.Fatalf("out=%v", out)
	}
	if _, ok := out["preview"].(string); !ok {
		t.Fatalf("preview missing: %v", out)
	}
}

func TestPrepareEventData_unknownTypeSafe(t *testing.T) {
	type secret struct{ Token string }
	data := map[string]any{"x": secret{Token: "hidden"}}
	out, err := PrepareEventData(data, nil, DefaultRedactionOptions())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "hidden") {
		t.Fatalf("leaked secret via repr: %s", b)
	}
	if !strings.Contains(string(b), "unserialized") {
		t.Fatalf("expected placeholder: %s", b)
	}
}

func TestPrepareEventData_mergesExtraRedactKeys(t *testing.T) {
	data := map[string]any{"custom_secret_field": "x"}
	out, err := PrepareEventData(data, []string{"custom_secret"}, DefaultRedactionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if out["custom_secret_field"] != RedactedPlaceholder {
		t.Fatalf("out=%v", out)
	}
}

func TestRecorder_Append_redactsBeforeStorage(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "redact.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	rec := NewRecorder(st)
	rec.Redaction = DefaultRedactionOptions()
	runID := "run-redact"
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "wf", Env: "local", Status: "running",
		StartedAt: time.Now().UTC(), InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Append(ctx, runID, "s1", EventToolCalled, map[string]any{
		"token": "abc",
	}); err != nil {
		t.Fatal(err)
	}
	events, err := st.ListTraceEventsByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%d", len(events))
	}
	if strings.Contains(events[0].DataJSON, "abc") {
		t.Fatalf("secret leaked: %s", events[0].DataJSON)
	}
	if !strings.Contains(events[0].DataJSON, RedactedPlaceholder) {
		t.Fatalf("data=%s", events[0].DataJSON)
	}
}
