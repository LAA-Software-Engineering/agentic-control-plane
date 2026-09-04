package trace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
)

// TestTruncateString_keepsValidUTF8 proves the stored preview stays valid UTF-8
// across byte budgets over multi-byte text (#386).
func TestTruncateString_keepsValidUTF8(t *testing.T) {
	s := strings.Repeat("日本語", 50)
	for max := 1; max <= len(s); max += 3 {
		got := truncateString(s, max)
		if !utf8.ValidString(got) {
			t.Fatalf("truncateString(max=%d) invalid UTF-8: %q", max, got)
		}
	}
}

// TestTruncatePayload_keepsValidUTF8 proves the JSON preview is cut on a rune
// boundary, not mid-rune (#386).
func TestTruncatePayload_keepsValidUTF8(t *testing.T) {
	data := map[string]any{"text": strings.Repeat("😀", 100)}
	out := truncatePayload(data, 64)
	preview, ok := out[FieldPayloadPreview].(string)
	if !ok {
		t.Fatalf("expected a truncated preview, got %#v", out)
	}
	if !utf8.ValidString(preview) {
		t.Fatalf("truncated payload preview is not valid UTF-8: %q", preview)
	}
}

func TestPrepareEventData_redactsNestedKeys(t *testing.T) {
	t.Parallel()
	data := map[string]any{
		"headers": map[string]any{
			"Authorization": "Bearer secret-token",
		},
		"api_key": "sk-live",
	}
	out := PrepareEventData(data, nil, DefaultRedactionOptions())
	headers, _ := out["headers"].(map[string]any)
	if headers["Authorization"] != RedactedPlaceholder {
		t.Fatalf("Authorization=%v", headers["Authorization"])
	}
	if out["api_key"] != RedactedPlaceholder {
		t.Fatalf("api_key=%v", out["api_key"])
	}
}

func TestPrepareEventData_maxDepth(t *testing.T) {
	t.Parallel()
	nested := map[string]any{}
	cur := nested
	for i := 0; i < 80; i++ {
		next := map[string]any{}
		cur["child"] = next
		cur = next
	}
	opts := DefaultRedactionOptions()
	opts.MaxDepth = 5
	out := PrepareEventData(nested, nil, opts)
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), "max depth 5 exceeded") {
		t.Fatalf("depth marker missing: %s", b)
	}
}

func TestPrepareEventData_truncatesPayload(t *testing.T) {
	t.Parallel()
	data := map[string]any{"blob": strings.Repeat("x", 200)}
	opts := DefaultRedactionOptions()
	opts.MaxPayloadBytes = 40
	out := PrepareEventData(data, nil, opts)
	if out[FieldPayloadTruncated] != true {
		t.Fatalf("out=%v", out)
	}
	if _, ok := out[FieldPayloadPreview].(string); !ok {
		t.Fatalf("preview missing: %v", out)
	}
}

func TestPrepareEventData_unknownTypeSafe(t *testing.T) {
	t.Parallel()
	type secret struct{ Token string }
	data := map[string]any{"x": secret{Token: "hidden"}}
	out := PrepareEventData(data, nil, DefaultRedactionOptions())
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "hidden") {
		t.Fatalf("leaked secret via repr: %s", b)
	}
	if !strings.Contains(string(b), "unserialized") {
		t.Fatalf("expected placeholder: %s", b)
	}
}

// TestPrepareEventData_typedScalarsAndSlices is the regression for issue #376: named string types
// (spec.HitlDecisionKind), typed slices ([]string, []spec.HitlDecisionKind), and string-keyed maps
// (map[string]string) must serialize to their values, not a "<type: unserialized>" placeholder, so
// the audit chain records which HITL decision the operator submitted and which were offered.
func TestPrepareEventData_typedScalarsAndSlices(t *testing.T) {
	t.Parallel()
	out := PrepareEventData(map[string]any{
		"decision":         spec.HitlDecisionApprove,
		"allowedDecisions": []spec.HitlDecisionKind{spec.HitlDecisionApprove, spec.HitlDecisionReject},
		"allowedSwitchTo":  []string{"tool.publish.default"},
		"toolCallIds":      []string{"call_1"},
		"labels":           map[string]string{"env": "prod"},
	}, nil, DefaultRedactionOptions())

	if out["decision"] != string(spec.HitlDecisionApprove) {
		t.Fatalf("decision = %v, want %q", out["decision"], spec.HitlDecisionApprove)
	}
	ad, ok := out["allowedDecisions"].([]any)
	if !ok || len(ad) != 2 || ad[0] != string(spec.HitlDecisionApprove) || ad[1] != string(spec.HitlDecisionReject) {
		t.Fatalf("allowedDecisions = %#v", out["allowedDecisions"])
	}
	sw, ok := out["allowedSwitchTo"].([]any)
	if !ok || len(sw) != 1 || sw[0] != "tool.publish.default" {
		t.Fatalf("allowedSwitchTo = %#v", out["allowedSwitchTo"])
	}
	ids, ok := out["toolCallIds"].([]any)
	if !ok || len(ids) != 1 || ids[0] != "call_1" {
		t.Fatalf("toolCallIds = %#v", out["toolCallIds"])
	}
	labels, ok := out["labels"].(map[string]any)
	if !ok || labels["env"] != "prod" {
		t.Fatalf("labels = %#v", out["labels"])
	}

	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "unserialized") {
		t.Fatalf("typed payload fields must serialize by value, got %s", b)
	}
}

// A redact key nested inside a typed (map[string]string) value is still masked: sanitizeReflect
// normalizes the map to map[string]any so the redaction pass reaches its keys (issue #376).
func TestPrepareEventData_redactsInsideTypedMap(t *testing.T) {
	t.Parallel()
	out := PrepareEventData(map[string]any{
		"headers": map[string]string{"Authorization": "Bearer sk-live"},
	}, nil, DefaultRedactionOptions())
	headers, ok := out["headers"].(map[string]any)
	if !ok || headers["Authorization"] != RedactedPlaceholder {
		t.Fatalf("Authorization not redacted through typed map: %#v", out["headers"])
	}
}

func TestPrepareEventData_mergesExtraRedactKeys(t *testing.T) {
	t.Parallel()
	data := map[string]any{"custom_secret_field": "x"}
	out := PrepareEventData(data, []string{"custom_secret"}, DefaultRedactionOptions())
	if out["custom_secret_field"] != RedactedPlaceholder {
		t.Fatalf("out=%v", out)
	}
}

func TestPrepareEventData_binaryHexPreview(t *testing.T) {
	t.Parallel()
	data := map[string]any{"raw": []byte{0xde, 0xad, 0xbe, 0xef}}
	out := PrepareEventData(data, nil, DefaultRedactionOptions())
	s, ok := out["raw"].(string)
	if !ok {
		t.Fatalf("raw=%T %v", out["raw"], out["raw"])
	}
	if !strings.Contains(s, "deadbeef") {
		t.Fatalf("expected hex preview: %q", s)
	}
	if strings.Contains(s, "\xde") {
		t.Fatalf("raw bytes in string: %q", s)
	}
}

func TestPrepareEventData_nonRedactKeyPreservesValue(t *testing.T) {
	t.Parallel()
	out := PrepareEventData(map[string]any{"x": 1}, nil, DefaultRedactionOptions())
	if out["x"] != float64(1) && out["x"] != int(1) {
		// json numbers may decode as float64 in some paths; here sanitize keeps int
		if out["x"] != 1 {
			t.Fatalf("x=%v", out["x"])
		}
	}
}

func TestTruncateString_shortMax(t *testing.T) {
	t.Parallel()
	if got := truncateString("abcdef", 3); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := truncateString("abcdef", 2); len(got) != 2 {
		t.Fatalf("got %q", got)
	}
}

func TestKeyMatchesRedact_substringSemantics(t *testing.T) {
	t.Parallel()
	keys := []string{"auth"}
	if !keyMatchesRedact("authorization", keys) {
		t.Fatal("expected authorization to match auth pattern")
	}
	// Substring match is security-biased: "author" contains "auth".
	if !keyMatchesRedact("author", keys) {
		t.Fatal("expected author to match auth pattern via substring")
	}
	if keyMatchesRedact("message", keys) {
		t.Fatal("message should not match")
	}
}

func TestMergeRedactKeys_dedupesAndTrims(t *testing.T) {
	t.Parallel()
	got := mergeRedactKeys([]string{" Token ", "token"}, []string{"", "API_KEY"})
	if len(got) != 2 {
		t.Fatalf("got=%v", got)
	}
}

func TestRedactArgsDiff_masksSensitivePathsAndNestedValues(t *testing.T) {
	t.Parallel()
	diff := map[string]any{
		"api_key": map[string]any{"from": "old-key", "to": "new-key"},
		"message": map[string]any{"from": "hello", "to": "world"},
		"nested.config": map[string]any{
			"from": map[string]any{"api_key": "secret-a"},
			"to":   map[string]any{"api_key": "secret-b"},
		},
	}
	out := RedactArgsDiff(diff, nil, DefaultRedactionOptions())
	api, _ := out["api_key"].(map[string]any)
	if api["from"] != RedactedPlaceholder || api["to"] != RedactedPlaceholder {
		t.Fatalf("api_key diff=%v", api)
	}
	nested, _ := out["nested.config"].(map[string]any)
	from, _ := nested["from"].(map[string]any)
	if from["api_key"] != RedactedPlaceholder {
		t.Fatalf("nested from=%v", nested)
	}
	msg, _ := out["message"].(map[string]any)
	if msg["from"] != "hello" || msg["to"] != "world" {
		t.Fatalf("benign path should keep values: %v", msg)
	}
}

func TestRecorder_Append_redactsBeforeStorage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "redact.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	rec := NewRecorder(st)
	runID := "run-redact"
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "wf", Env: "local", Status: "running",
		StartedAt: time.Now().UTC(), InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Append(ctx, runID, "s1", EventToolSelection, ActorAgent, map[string]any{
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

func TestNewRecorderForGraph_Append_respectsMaxPayloadBytes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "graph-rec.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Traces: &spec.ProjectTracesConfig{MaxPayloadBytes: 30},
		},
	}
	rec := NewRecorderForGraph(st, g)
	runID := "run-graph"
	if err := st.StartRun(ctx, state.Run{
		RunID: runID, WorkflowName: "wf", Env: "local", Status: "running",
		StartedAt: time.Now().UTC(), InputJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Append(ctx, runID, "", EventRunStarted, ActorAgent, map[string]any{
		"blob": strings.Repeat("z", 200),
	}); err != nil {
		t.Fatal(err)
	}
	events, err := st.ListTraceEventsByRunID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(events[0].DataJSON, FieldPayloadTruncated) {
		t.Fatalf("data=%s", events[0].DataJSON)
	}
}
