package mcpserver

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
//
// This handler is UNAUTHENTICATED: it carries the run's authority (tools/call routes into the live
// PolicyDispatcher and budget), and loopback only stops *off-host* access — every process on the
// same host can reach 127.0.0.1, so on a shared host any local user could drive the run's granted
// operations. Do not expose it bare. Use [Server.ListenLocal], which binds it to a per-run bearer
// token; a caller embedding this handler directly must add its own authentication.
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
// nothing off-host can reach it) bound to a freshly minted per-run bearer token, and returns the
// [Transport] to hand to [MCPConfigJSON]/[WriteMCPConfig] (URL + Authorization header) plus a stop
// function. The token is what actually confines the endpoint: loopback keeps it off-host, and the
// token keeps *same-host* processes out, binding it to the agent Terfyn spawned with this
// --mcp-config. The caller must invoke stop when the run ends.
func (s *Server) ListenLocal() (Transport, func(), error) {
	token, err := randomBearerToken()
	if err != nil {
		return Transport{}, nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Transport{}, nil, err
	}
	srv := &http.Server{Handler: requireBearer(token, s.HTTPHandler())}
	go func() { _ = srv.Serve(ln) }()
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	tr := Transport{
		URL:     "http://" + ln.Addr().String() + "/mcp",
		Headers: map[string]string{"Authorization": "Bearer " + token},
	}
	return tr, stop, nil
}

// requireBearer wraps h, rejecting (401) any request whose Authorization header is not exactly
// "Bearer <token>", compared in constant time so a mismatch does not leak the token by timing. An
// empty token disables the check (h is returned unwrapped).
func requireBearer(token string, h http.Handler) http.Handler {
	if token == "" {
		return h
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			http.Error(w, "mcpserver: unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// randomBearerToken returns a 256-bit URL-safe random token.
func randomBearerToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("mcpserver: generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
