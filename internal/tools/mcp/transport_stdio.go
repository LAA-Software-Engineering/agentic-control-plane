package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// StdioTransport runs an MCP server over newline-delimited JSON-RPC on stdin/stdout.
type StdioTransport struct {
	command string
	args    []string

	cmd   *exec.Cmd
	stdin io.WriteCloser
	br    *bufio.Reader

	nextID int64
	mu     sync.Mutex
}

// NewStdioTransport configures a stdio MCP session (Start not yet called).
func NewStdioTransport(command string, args []string) *StdioTransport {
	cp := append([]string(nil), args...)
	return &StdioTransport{command: command, args: cp}
}

// Start launches the subprocess and wires stdio pipes.
func (t *StdioTransport) Start(ctx context.Context) error {
	if t == nil {
		return fmt.Errorf("mcp: nil transport")
	}
	t.cmd = exec.CommandContext(ctx, t.command, t.args...)
	in, err := t.cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := t.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	t.stdin = in
	t.br = bufio.NewReader(out)
	t.cmd.Stderr = io.Discard
	return t.cmd.Start()
}

// Close ends the session (best-effort kill).
func (t *StdioTransport) Close() error {
	if t == nil {
		return nil
	}
	if t.stdin != nil {
		_ = t.stdin.Close()
		t.stdin = nil
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
		t.cmd = nil
	}
	return nil
}

func (t *StdioTransport) writeMessage(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := t.stdin.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

func (t *StdioTransport) readLineCtx(ctx context.Context) ([]byte, error) {
	type res struct {
		line []byte
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		line, err := t.br.ReadBytes('\n')
		ch <- res{line, err}
	}()
	select {
	case <-ctx.Done():
		if t.cmd != nil && t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
		return nil, ctx.Err()
	case r := <-ch:
		return r.line, r.err
	}
}

// RoundTrip sends a JSON-RPC request and returns the result field for the matching id.
func (t *StdioTransport) RoundTrip(ctx context.Context, method string, params any) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	id := atomic.AddInt64(&t.nextID, 1)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	if err := t.writeMessage(req); err != nil {
		return nil, err
	}

	for {
		line, err := t.readLineCtx(ctx)
		if err != nil {
			return nil, err
		}
		var msg map[string]any
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if _, hasMethod := msg["method"].(string); hasMethod && msg["id"] == nil {
			continue
		}
		rid, ok := msg["id"]
		if !ok {
			continue
		}
		if !jsonRPCIDMatches(rid, id) {
			continue
		}
		if errObj, ok := msg["error"]; ok && errObj != nil {
			return nil, rpcErrorf("rpc error: %v", errObj)
		}
		raw, err := json.Marshal(msg["result"])
		if err != nil {
			return nil, err
		}
		return json.RawMessage(raw), nil
	}
}

func jsonRPCIDMatches(rid any, want int64) bool {
	switch x := rid.(type) {
	case float64:
		return int64(x) == want
	case json.Number:
		n, err := x.Int64()
		return err == nil && n == want
	case int64:
		return x == want
	default:
		return false
	}
}
