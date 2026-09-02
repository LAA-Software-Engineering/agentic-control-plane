package mcpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postRPC(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestHTTPHandler_initializeAndToolsList(t *testing.T) {
	h := newTestServer(&fakeDispatcher{}).HTTPHandler()

	rr := postRPC(t, h, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("initialize status = %d", rr.Code)
	}
	var init map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &init); err != nil {
		t.Fatal(err)
	}
	if init["result"].(map[string]any)["protocolVersion"] != protocolVersion {
		t.Fatalf("initialize result: %v", init)
	}

	rr = postRPC(t, h, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	var list map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	tools := list["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	if len(names) != 2 || !names["workspace_read_file"] || !names["workspace_run_tests"] {
		t.Fatalf("tools/list over HTTP = %v", names)
	}
}

func TestHTTPHandler_toolsCallRoutesThroughDispatcher(t *testing.T) {
	fd := &fakeDispatcher{out: map[string]any{"ok": true}}
	h := newTestServer(fd).HTTPHandler()
	rr := postRPC(t, h, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"workspace_read_file","arguments":{"path":"main.go"}}}`)
	if fd.gotUses != "tool.workspace.read_file" || fd.gotArgs["path"] != "main.go" {
		t.Fatalf("dispatcher not reached: uses=%q args=%v", fd.gotUses, fd.gotArgs)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestHTTPHandler_notificationIsAccepted(t *testing.T) {
	h := newTestServer(&fakeDispatcher{}).HTTPHandler()
	rr := postRPC(t, h, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("notification status = %d, want 202", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Fatalf("notification must have no body, got %q", rr.Body.String())
	}
}

func TestHTTPHandler_rejectsNonPost(t *testing.T) {
	h := newTestServer(&fakeDispatcher{}).HTTPHandler()
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", rr.Code)
	}
}

func TestHTTPHandler_malformedBodyIsParseError(t *testing.T) {
	h := newTestServer(&fakeDispatcher{}).HTTPHandler()
	rr := postRPC(t, h, `not json`)
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] == nil {
		t.Fatalf("malformed body must be a JSON-RPC error, got %v", resp)
	}
}

// End-to-end over a real loopback listener: a client reaches the per-run server the way the
// external agent would.
func TestListenLocal_endToEnd(t *testing.T) {
	fd := &fakeDispatcher{out: map[string]any{"echo": "hi"}}
	url, stop, err := newTestServer(fd).ListenLocal()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Fatalf("loopback url = %q", url)
	}

	resp, err := http.Post(url, "application/json",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"workspace_run_tests","arguments":{}}}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if fd.gotUses != "tool.workspace.run_tests" {
		t.Fatalf("dispatcher over loopback got uses=%q", fd.gotUses)
	}
}
