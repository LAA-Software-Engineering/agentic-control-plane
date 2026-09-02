package mcpserver

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"
)

// maxHTTPBody caps a single JSON-RPC request body (tool inputs, final text).
const maxHTTPBody = 16 << 20

// HTTPHandler serves the per-run MCP server over HTTP — the MCP "streamable HTTP" transport, one
// JSON-RPC message per POST, response returned as application/json (a notification, which carries
// no id, gets 202 Accepted with no body). This is the transport the external Claude Code runtime
// uses (issue #367): the per-run server stays IN the Terfyn process, with direct access to the
// run's PolicyDispatcher (policy → CheckToolCall → Tools.Call), and the external agent reaches it
// over loopback — rather than a stdio subprocess that would have to re-hydrate the run's authority.
// Register it in --mcp-config as { "type": "http", "url": <ListenLocal url> } with
// --strict-mcp-config, so it is the only MCP server the agent sees.
func (s *Server) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "mcpserver: only POST is supported", http.StatusMethodNotAllowed)
			return
		}
		var req jsonRPCRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxHTTPBody)).Decode(&req); err != nil {
			// A malformed body is a JSON-RPC parse error (protocol-level), not an HTTP error.
			writeJSONRPC(w, jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      json.RawMessage("null"),
				Error:   &jsonRPCError{Code: codeParseError, Message: "parse error"},
			})
			return
		}
		resp, reply := s.handle(r.Context(), req)
		if !reply {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSONRPC(w, resp)
	})
}

func writeJSONRPC(w http.ResponseWriter, resp jsonRPCResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// ListenLocal starts the MCP HTTP server on a loopback address (127.0.0.1:0, an ephemeral port, so
// nothing off-host can reach it) and returns the URL to put in --mcp-config plus a stop function.
// The caller must invoke stop when the run ends.
func (s *Server) ListenLocal() (url string, stop func(), err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	srv := &http.Server{Handler: s.HTTPHandler()}
	go func() { _ = srv.Serve(ln) }()
	stop = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	return "http://" + ln.Addr().String() + "/mcp", stop, nil
}
