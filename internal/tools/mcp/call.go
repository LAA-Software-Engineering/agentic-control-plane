package mcp

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// ExecMeta is timing/cost metadata for an MCP call (§13.2 placeholders).
type ExecMeta struct {
	DurationMs int64
	CostUSD    float64
}

// CallStdio runs one MCP tools/call over a fresh stdio subprocess, with optional retries on transport errors (§13.4).
func CallStdio(ctx context.Context, cfg *spec.ToolMCP, retry *spec.ToolRetry, toolName string, arguments map[string]any) (map[string]any, ExecMeta, error) {
	if cfg == nil {
		return nil, ExecMeta{}, errors.New("mcp: nil mcp config")
	}
	if strings.ToLower(strings.TrimSpace(cfg.Transport)) != "stdio" {
		return nil, ExecMeta{}, errors.New("mcp: only transport stdio is supported in MVP")
	}
	cmd := strings.TrimSpace(cfg.Command)
	if cmd == "" {
		return nil, ExecMeta{}, errors.New("mcp: empty command")
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

	if err := tr.Initialize(ctx); err != nil {
		return nil, err
	}
	return tr.CallTool(ctx, toolName, arguments)
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
