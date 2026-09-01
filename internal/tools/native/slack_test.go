package native

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type slackStub struct {
	path string
	body map[string]any
}

func newSlackStub(t *testing.T, respBody string) *slackStub {
	t.Helper()
	s := &slackStub{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.path = r.URL.Path
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			_ = json.Unmarshal(b, &s.body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, respBody) // Slack always replies 200
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("SLACK_API_URL", srv.URL)
	return s
}

func TestSlackMessageSend_happyPath(t *testing.T) {
	stub := newSlackStub(t, `{"ok":true,"ts":"1727.0001","channel":"C123","message":{"dropped":true}}`)
	out, err := slackMessageSend(context.Background(), map[string]any{"channel": "C123", "text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if stub.path != "/chat.postMessage" || stub.body["channel"] != "C123" || stub.body["text"] != "hello" {
		t.Fatalf("request %s payload %#v", stub.path, stub.body)
	}
	if out["ts"] != "1727.0001" || out["channel"] != "C123" {
		t.Fatalf("out %#v", out)
	}
	if _, leaked := out["message"]; leaked {
		t.Fatalf("result should be curated, got %#v", out)
	}
}

func TestSlackMessageSend_threadTs(t *testing.T) {
	stub := newSlackStub(t, `{"ok":true,"ts":"2.0","channel":"C1"}`)
	if _, err := slackMessageSend(context.Background(), map[string]any{"channel": "C1", "text": "reply", "thread_ts": "1.0"}); err != nil {
		t.Fatal(err)
	}
	if stub.body["thread_ts"] != "1.0" {
		t.Fatalf("thread_ts not forwarded: %#v", stub.body)
	}
}

// TestSlackMessageSend_okFalseIsError is the Slack-specific contract: HTTP 200 with ok=false
// (e.g. channel_not_found) must surface as an error, not a success.
func TestSlackMessageSend_okFalseIsError(t *testing.T) {
	newSlackStub(t, `{"ok":false,"error":"channel_not_found"}`)
	_, err := slackMessageSend(context.Background(), map[string]any{"channel": "nope", "text": "x"})
	if err == nil || !strings.Contains(err.Error(), "channel_not_found") {
		t.Fatalf("expected a channel_not_found error, got %v", err)
	}
}

func TestSlackMessageUpdate_happyPath(t *testing.T) {
	stub := newSlackStub(t, `{"ok":true,"ts":"9.9","channel":"C7"}`)
	if _, err := slackMessageUpdate(context.Background(), map[string]any{"channel": "C7", "ts": "9.9", "text": "edited"}); err != nil {
		t.Fatal(err)
	}
	if stub.path != "/chat.update" || stub.body["ts"] != "9.9" || stub.body["text"] != "edited" {
		t.Fatalf("request %s payload %#v", stub.path, stub.body)
	}
}

func TestSlackMessageSend_missingArgs(t *testing.T) {
	newSlackStub(t, `{"ok":true}`)
	if _, err := slackMessageSend(context.Background(), map[string]any{"channel": "C1"}); err == nil {
		t.Fatal("expected an error for missing text")
	}
	if _, err := slackMessageUpdate(context.Background(), map[string]any{"channel": "C1", "text": "x"}); err == nil {
		t.Fatal("expected an error for missing ts")
	}
}

func TestSlack_requiresToken(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "")
	if _, err := slackMessageSend(context.Background(), map[string]any{"channel": "C1", "text": "x"}); err == nil || !strings.Contains(err.Error(), "SLACK_BOT_TOKEN") {
		t.Fatalf("expected a SLACK_BOT_TOKEN error, got %v", err)
	}
}
