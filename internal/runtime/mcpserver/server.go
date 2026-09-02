package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// protocolVersion is the MCP protocol revision the server advertises in initialize.
const protocolVersion = "2024-11-05"

// Server serves one agent's closed callable world over MCP (JSON-RPC 2.0, newline-delimited
// framing on stdio). It advertises exactly the compiled grants in tools/list and routes every
// tools/call through its Dispatcher. It never widens the set from a live source: a tools/call
// for a name outside the compiled set is refused without reaching the dispatcher.
type Server struct {
	name       string
	ops        []GrantedOp
	byName     map[string]GrantedOp
	dispatch   Dispatcher
	serverName string
}

// NewServer builds a server for a compiled grant set. serverName is the MCP server label
// (the key under which it is registered in --mcp-config); it defaults to "terfyn".
func NewServer(cs CompiledServer, d Dispatcher, serverName string) *Server {
	if serverName == "" {
		serverName = "terfyn"
	}
	byName := make(map[string]GrantedOp, len(cs.Ops))
	for _, op := range cs.Ops {
		byName[op.MCPName] = op
	}
	return &Server{name: cs.Agent, ops: cs.Ops, byName: byName, dispatch: d, serverName: serverName}
}

// jsonRPCRequest is one incoming JSON-RPC message. A request carries an id; a notification
// omits it (id stays nil) and gets no response.
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC error codes used by the server (a subset of the spec's reserved range).
const (
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// Serve reads newline-delimited JSON-RPC from r and writes responses to w until r is
// exhausted or ctx is cancelled. Notifications (no id) are handled without a reply.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	enc := json.NewEncoder(w)
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue // a malformed frame is skipped rather than tearing down the session
		}
		resp, reply := s.handle(ctx, req)
		if !reply {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("mcpserver: write response: %w", err)
		}
	}
	return sc.Err()
}

// handle dispatches one request. The second return is false for notifications, which get no
// response.
func (s *Server) handle(ctx context.Context, req jsonRPCRequest) (jsonRPCResponse, bool) {
	// A notification (no id) never gets a response, whatever the method.
	isNotification := len(req.ID) == 0
	switch req.Method {
	case "initialize":
		return s.ok(req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.serverName, "version": "0"},
		}), !isNotification
	case "notifications/initialized", "notifications/cancelled":
		return jsonRPCResponse{}, false
	case "ping":
		return s.ok(req.ID, map[string]any{}), !isNotification
	case "tools/list":
		return s.ok(req.ID, map[string]any{"tools": s.toolList()}), !isNotification
	case "tools/call":
		if isNotification {
			return jsonRPCResponse{}, false
		}
		return s.callTool(ctx, req), true
	default:
		if isNotification {
			return jsonRPCResponse{}, false
		}
		return s.fail(req.ID, codeMethodNotFound, fmt.Sprintf("method %q not found", req.Method)), true
	}
}

// toolList renders the compiled grants as MCP tool descriptors. inputSchema is a permissive
// object schema: per-operation input-schema resolution is out of scope for the closed-world
// surface (ADR 005 §5) — the callable *set* is the boundary this server enforces.
func (s *Server) toolList() []map[string]any {
	list := make([]map[string]any, 0, len(s.ops))
	for _, op := range s.ops {
		list = append(list, map[string]any{
			"name":        op.MCPName,
			"description": toolDescription(op),
			"inputSchema": map[string]any{"type": "object"},
		})
	}
	return list
}

func toolDescription(op GrantedOp) string {
	desc := fmt.Sprintf("Terfyn granted operation %s (%s)", op.Uses, op.Operation)
	if len(op.Effects) > 0 {
		desc += fmt.Sprintf("; effects: %v", op.Effects)
	}
	return desc
}

type callParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// callTool routes a tools/call through the dispatcher. A name outside the compiled set is
// refused here — the closed world is enforced before any dispatch. A dispatch error (policy
// denial, executor failure) is returned to the agent as an isError tool result so the model
// sees the refusal, while Terfyn's own trace records the denial separately.
func (s *Server) callTool(ctx context.Context, req jsonRPCRequest) jsonRPCResponse {
	var p callParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return s.fail(req.ID, codeInvalidParams, fmt.Sprintf("invalid tools/call params: %v", err))
		}
	}
	op, ok := s.byName[p.Name]
	if !ok {
		return s.fail(req.ID, codeInvalidParams, fmt.Sprintf("unknown tool %q: not in this run's granted capabilities", p.Name))
	}
	out, err := s.dispatch.Call(ctx, op.Uses, p.Arguments)
	if err != nil {
		return s.ok(req.ID, toolResult(fmt.Sprintf("tool call denied or failed: %v", err), true))
	}
	return s.ok(req.ID, toolResult(marshalContent(out), false))
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

func marshalContent(out map[string]any) string {
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Sprintf("%v", out)
	}
	return string(b)
}

func (s *Server) ok(id json.RawMessage, result any) jsonRPCResponse {
	return jsonRPCResponse{JSONRPC: "2.0", ID: idOrNull(id), Result: result}
}

func (s *Server) fail(id json.RawMessage, code int, msg string) jsonRPCResponse {
	return jsonRPCResponse{JSONRPC: "2.0", ID: idOrNull(id), Error: &jsonRPCError{Code: code, Message: msg}}
}

func idOrNull(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}
