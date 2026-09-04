package mcp

import "testing"

// TestJSONRPCResultFromMap_serverRequestNotMatched proves a server-initiated request whose id
// collides with our outstanding call id is NOT treated as our response — it carries a `method`, so
// it is a request/notification, not a result (#397). Before the fix it matched and returned "null".
func TestJSONRPCResultFromMap_serverRequestNotMatched(t *testing.T) {
	for _, method := range []string{"ping", "roots/list", "sampling/createMessage"} {
		msg := map[string]any{"jsonrpc": "2.0", "id": float64(1), "method": method}
		raw, matched, err := jsonRPCResultFromMap(msg, 1)
		if matched {
			t.Fatalf("server request %q with colliding id was consumed as our response (raw=%q)", method, raw)
		}
		if err != nil {
			t.Fatalf("server request %q should be skipped without error, got %v", method, err)
		}
	}
}

// TestJSONRPCResultFromMap_matchedWithoutResultOrError proves a matched response carrying neither
// result nor error is rejected rather than reported as a success with `null`/empty output (#397).
func TestJSONRPCResultFromMap_matchedWithoutResultOrError(t *testing.T) {
	msg := map[string]any{"jsonrpc": "2.0", "id": float64(1)}
	_, matched, err := jsonRPCResultFromMap(msg, 1)
	if !matched {
		t.Fatal("a message with our id must be recognized as our response")
	}
	if err == nil {
		t.Fatal("a response with neither result nor error must be rejected")
	}
}

// TestJSONRPCResultFromMap_genuineResponseMatches is the positive control: a real response with a
// matching id and a result is matched and returned.
func TestJSONRPCResultFromMap_genuineResponseMatches(t *testing.T) {
	msg := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": map[string]any{"ok": true}}
	raw, matched, err := jsonRPCResultFromMap(msg, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Fatal("a genuine response with a matching id must match")
	}
	if string(raw) != `{"ok":true}` {
		t.Fatalf("result = %s", raw)
	}
}
