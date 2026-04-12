package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// ExecMeta is timing/cost metadata for an MCP call (§13.2 placeholders).
type ExecMeta struct {
	DurationMs int64
	CostUSD    float64
}

// Call runs one MCP tools/call: stdio subprocess or HTTP endpoint per spec.mcp.transport (issue #77).
func Call(ctx context.Context, cfg *spec.ToolMCP, retry *spec.ToolRetry, toolName string, arguments map[string]any) (map[string]any, ExecMeta, error) {
	if cfg == nil {
		return nil, ExecMeta{}, errors.New("mcp: nil mcp config")
	}
	trans := strings.ToLower(strings.TrimSpace(cfg.Transport))
	switch trans {
	case "stdio":
		return callStdioLoop(ctx, cfg, retry, toolName, arguments)
	case "http":
		return callHTTPLoop(ctx, cfg, retry, toolName, arguments)
	default:
		return nil, ExecMeta{}, fmt.Errorf("mcp: unsupported transport %q (stdio or http)", cfg.Transport)
	}
}

func callStdioLoop(ctx context.Context, cfg *spec.ToolMCP, retry *spec.ToolRetry, toolName string, arguments map[string]any) (map[string]any, ExecMeta, error) {
	cmd := strings.TrimSpace(cfg.Command)
	if cmd == "" {
		return nil, ExecMeta{}, errors.New("mcp: stdio transport requires command")
	}
	attempts := 1
	if retry != nil && retry.MaxAttempts > 0 {
		attempts = retry.MaxAttempts
	}
	backoff := ""
	if retry != nil {
		backoff = retry.Backoff
	}
	startAll := time.Now()
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			sleepBackoff(ctx, attempt, backoff)
		}
		out, err := oneStdioAttempt(ctx, cmd, cfg.Args, toolName, arguments)
		if err == nil {
			return out, ExecMeta{DurationMs: time.Since(startAll).Milliseconds(), CostUSD: 0}, nil
		}
		lastErr = err
		if !retryableTransportErr(err) {
			break
		}
	}
	return nil, ExecMeta{DurationMs: time.Since(startAll).Milliseconds(), CostUSD: 0}, lastErr
}

func oneStdioAttempt(ctx context.Context, command string, args []string, toolName string, arguments map[string]any) (map[string]any, error) {
	tr := NewStdioTransport(command, args)
	if err := tr.Start(ctx); err != nil {
		return nil, err
	}
	defer tr.Close()

	if err := Initialize(ctx, tr); err != nil {
		return nil, err
	}
	return CallTool(ctx, tr, toolName, arguments)
}

func callHTTPLoop(ctx context.Context, cfg *spec.ToolMCP, retry *spec.ToolRetry, toolName string, arguments map[string]any) (map[string]any, ExecMeta, error) {
	u := strings.TrimSpace(cfg.URL)
	if u == "" {
		return nil, ExecMeta{}, errors.New("mcp: http transport requires url")
	}
	attempts := 1
	if retry != nil && retry.MaxAttempts > 0 {
		attempts = retry.MaxAttempts
	}
	backoff := ""
	if retry != nil {
		backoff = retry.Backoff
	}
	startAll := time.Now()
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			sleepBackoff(ctx, attempt, backoff)
		}
		out, err := oneHTTPAttempt(ctx, cfg, nil, toolName, arguments)
		if err == nil {
			return out, ExecMeta{DurationMs: time.Since(startAll).Milliseconds(), CostUSD: 0}, nil
		}
		lastErr = err
		if !retryableTransportErr(err) {
			break
		}
	}
	return nil, ExecMeta{DurationMs: time.Since(startAll).Milliseconds(), CostUSD: 0}, lastErr
}

func oneHTTPAttempt(ctx context.Context, cfg *spec.ToolMCP, client *http.Client, toolName string, arguments map[string]any) (map[string]any, error) {
	tr, err := NewHTTPTransport(cfg.URL, cfg.Headers, client)
	if err != nil {
		return nil, err
	}
	defer tr.Close()

	if err := Initialize(ctx, tr); err != nil {
		return nil, err
	}
	return CallTool(ctx, tr, toolName, arguments)
}

func retryableTransportErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var re *rpcError
	if errors.As(err, &re) {
		return false
	}
	return true
}

func sleepBackoff(ctx context.Context, attempt int, kind string) {
	if attempt <= 0 {
		return
	}
	var d time.Duration
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "exponential":
		shift := attempt
		if shift > 8 {
			shift = 8
		}
		d = time.Millisecond * time.Duration(50*(1<<shift))
	case "fixed":
		d = 100 * time.Millisecond
	default:
		d = 50 * time.Millisecond
	}
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
