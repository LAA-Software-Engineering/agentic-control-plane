package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeDispatcher records the uses it was asked to run and returns a canned result/error.
type fakeDispatcher struct {
	gotUses string
	gotArgs map[string]any
	out     map[string]any
	err     error
}

func (f *fakeDispatcher) Call(_ context.Context, uses string, args map[string]any) (map[string]any, error) {
	f.gotUses = uses
	f.gotArgs = args
	return f.out, f.err
}

func newTestServer(d Dispatcher) *Server {
	cs := CompiledServer{Agent: "Coder", Ops: []GrantedOp{
		{MCPName: "workspace_read_file", Uses: "tool.workspace.read_file", Tool: "workspace", Operation: "read_file"},
		{MCPName: "workspace_run_tests", Uses: "tool.workspace.run_tests", Tool: "workspace", Operation: "run_tests"},
	}}
	return NewServer(cs, d, "terfyn")
}

// drive feeds newline-delimited requests through Serve and returns the decoded responses.
func drive(t *testing.T, s *Server, requests ...string) []map[string]any {
	t.Helper()
	in := strings.NewReader(strings.Join(requests, "\n") + "\n")
	var out strings.Builder
	if err := s.Serve(context.Background(), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var resps []map[string]any
	dec := json.NewDecoder(strings.NewReader(out.String()))
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		resps = append(resps, m)
	}
	return resps
}

func TestServer_Initialize(t *testing.T) {
	s := newTestServer(&fakeDispatcher{})
	resps := drive(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if len(resps) != 1 {
		t.Fatalf("want 1 response, got %d", len(resps))
	}
	res := resps[0]["result"].(map[string]any)
	if res["protocolVersion"] != protocolVersion {
		t.Fatalf("protocolVersion: %v", res)
	}
}

// tools/list must return exactly the granted set (the "Done when").
func TestServer_ToolsList_ClosedWorld(t *testing.T) {
	s := newTestServer(&fakeDispatcher{})
	resps := drive(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := resps[0]["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	if len(names) != 2 || !names["workspace_read_file"] || !names["workspace_run_tests"] {
		t.Fatalf("tools/list = %v, want exactly the two grants", names)
	}
	if names["workspace_write_file"] {
		t.Fatal("ungranted tool leaked into tools/list")
	}
}

func TestServer_ToolsCall_RoutesToDispatcher(t *testing.T) {
	fd := &fakeDispatcher{out: map[string]any{"ok": true}}
	s := newTestServer(fd)
	resps := drive(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"workspace_read_file","arguments":{"path":"main.go"}}}`)
	if fd.gotUses != "tool.workspace.read_file" {
		t.Fatalf("dispatcher got uses %q", fd.gotUses)
	}
	if fd.gotArgs["path"] != "main.go" {
		t.Fatalf("dispatcher got args %v", fd.gotArgs)
	}
	res := resps[0]["result"].(map[string]any)
	if res["isError"] != false {
		t.Fatalf("expected success result, got %v", res)
	}
}

// A tools/call for a name outside the compiled set is refused before the dispatcher — the
// closed world holds even if the model asks for an ungranted tool by name.
func TestServer_ToolsCall_UnknownToolRefusedBeforeDispatch(t *testing.T) {
	fd := &fakeDispatcher{}
	s := newTestServer(fd)
	resps := drive(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"workspace_write_file","arguments":{}}}`)
	if fd.gotUses != "" {
		t.Fatal("dispatcher must not be reached for an ungranted tool")
	}
	if resps[0]["error"] == nil {
		t.Fatalf("unknown tool must be a JSON-RPC error, got %v", resps[0])
	}
}

// A dispatcher error (e.g. a policy denial) is surfaced to the agent as an isError result.
func TestServer_ToolsCall_DispatchErrorIsIsError(t *testing.T) {
	fd := &fakeDispatcher{err: errors.New("policy: denied")}
	s := newTestServer(fd)
	resps := drive(t, s, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"workspace_read_file","arguments":{}}}`)
	res := resps[0]["result"].(map[string]any)
	if res["isError"] != true {
		t.Fatalf("dispatch error must yield isError result, got %v", res)
	}
}

// A notification (no id) gets no response, whatever the method.
func TestServer_NotificationNoResponse(t *testing.T) {
	s := newTestServer(&fakeDispatcher{})
	resps := drive(t, s, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if len(resps) != 0 {
		t.Fatalf("notification must not get a response, got %v", resps)
	}
}

func TestServer_UnknownMethod(t *testing.T) {
	s := newTestServer(&fakeDispatcher{})
	resps := drive(t, s, `{"jsonrpc":"2.0","id":6,"method":"resources/list"}`)
	if resps[0]["error"] == nil {
		t.Fatalf("unknown method must be a JSON-RPC error, got %v", resps[0])
	}
}
