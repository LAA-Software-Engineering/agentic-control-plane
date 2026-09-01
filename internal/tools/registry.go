package tools

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Terfyn/terfyn/internal/spec"
	httptool "github.com/Terfyn/terfyn/internal/tools/http"
	"github.com/Terfyn/terfyn/internal/tools/mcp"
	"github.com/Terfyn/terfyn/internal/tools/native"
)

// Registry resolves workflow uses strings against declared tools and dispatches by transport (MVP: native, mock, mcp stdio, http).
type Registry struct {
	graph  *spec.ProjectGraph
	native *native.Registry
	// ProjectRoot resolves a relative spec.workspace.root for the native workspace adapter. Empty
	// leaves a relative root to resolve against the process working directory.
	ProjectRoot string
	// Mock is optional; when set, ToolSpec type "mock" delegates here. Otherwise a canned JSON body is returned.
	Mock ToolExecutor
}

// NewRegistry builds a registry from the merged project graph.
func NewRegistry(g *spec.ProjectGraph) *Registry {
	return &Registry{
		graph:  g,
		native: native.NewRegistry(),
	}
}

// NewRegistryWithRoot builds a registry that resolves a relative spec.workspace.root against
// projectRoot (issue #323 follow-up). Equivalent to NewRegistry with ProjectRoot set.
func NewRegistryWithRoot(g *spec.ProjectGraph, projectRoot string) *Registry {
	r := NewRegistry(g)
	r.ProjectRoot = projectRoot
	return r
}

// resolveWorkspaceRoot resolves a declared spec.workspace.root: an absolute path (or empty, for the
// env fallback) is returned unchanged; a relative path is resolved against the registry's ProjectRoot
// so the sandbox is reproducible regardless of the process working directory.
func (r *Registry) resolveWorkspaceRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" || filepath.IsAbs(root) || strings.TrimSpace(r.ProjectRoot) == "" {
		return root
	}
	return filepath.Join(r.ProjectRoot, root)
}

// ParseUses splits tool.github.pull_request.get into tool name "github" and operation "pull_request.get".
func ParseUses(uses string) (toolName string, operation string, err error) {
	uses = strings.TrimSpace(uses)
	const prefix = "tool."
	if !strings.HasPrefix(uses, prefix) {
		return "", "", fmt.Errorf("tools: uses %q must start with %q", uses, prefix)
	}
	rest := strings.TrimPrefix(uses, prefix)
	i := strings.IndexByte(rest, '.')
	if i <= 0 || i >= len(rest)-1 {
		return "", "", fmt.Errorf("tools: uses %q must be tool.<name>.<operation>", uses)
	}
	toolName = rest[:i]
	operation = rest[i+1:]
	if strings.TrimSpace(toolName) == "" || strings.TrimSpace(operation) == "" {
		return "", "", fmt.Errorf("tools: uses %q must be tool.<name>.<operation>", uses)
	}
	return toolName, operation, nil
}

// Call implements [ToolExecutor] by resolving Uses against the project graph.
func (r *Registry) Call(ctx context.Context, req ToolCallRequest) (ToolCallResponse, error) {
	if r == nil {
		return ToolCallResponse{}, fmt.Errorf("tools: nil registry")
	}
	start := time.Now()
	toolName, operation, err := ParseUses(req.Uses)
	if err != nil {
		return ToolCallResponse{}, err
	}
	if r.graph == nil || r.graph.Tools == nil {
		return ToolCallResponse{}, fmt.Errorf("tools: unknown tool %q", toolName)
	}
	tr, ok := r.graph.Tools[toolName]
	if !ok || tr == nil {
		return ToolCallResponse{}, fmt.Errorf("tools: unknown tool %q", toolName)
	}
	typ := strings.ToLower(strings.TrimSpace(tr.Spec.Type))
	switch typ {
	case "native":
		if r.native == nil {
			r.native = native.NewRegistry()
		}
		if ws := tr.Spec.Workspace; ws != nil {
			ctx = native.WithWorkspaceConfig(ctx, native.WorkspaceConfig{
				Root:        r.resolveWorkspaceRoot(ws.Root),
				TestCommand: ws.TestCommand,
			})
		}
		out, meta, err := r.native.Dispatch(ctx, operation, req.With)
		if err != nil {
			if errors.Is(err, native.ErrUnknownOperation) {
				return ToolCallResponse{}, &UnknownOperationError{Tool: toolName, Operation: operation}
			}
			return ToolCallResponse{}, err
		}
		return normalizeResponse(out, ToolCallMeta{DurationMs: meta.DurationMs, CostUSD: meta.CostUSD}, start), nil
	case "mock":
		if r.Mock != nil {
			return r.Mock.Call(ctx, req)
		}
		return normalizeResponse(
			map[string]any{"message": "mock", "uses": req.Uses},
			ToolCallMeta{DurationMs: 1, CostUSD: 0},
			start,
		), nil
	case "mcp":
		if tr.Spec.MCP == nil {
			return ToolCallResponse{}, fmt.Errorf("tools: mcp tool %q missing mcp configuration", toolName)
		}
		out, meta, err := mcp.Call(ctx, tr.Spec.MCP, tr.Spec.Retry, operation, req.With)
		if err != nil {
			return ToolCallResponse{}, err
		}
		return normalizeResponse(out, ToolCallMeta{DurationMs: meta.DurationMs, CostUSD: meta.CostUSD}, start), nil
	case "http":
		if tr.Spec.HTTP == nil {
			return ToolCallResponse{}, fmt.Errorf("tools: http tool %q missing http configuration", toolName)
		}
		out, meta, err := httptool.Execute(ctx, tr.Spec.HTTP, tr.Spec.Retry, operation, req.With, nil)
		if err != nil {
			return ToolCallResponse{}, err
		}
		return normalizeResponse(out, ToolCallMeta{DurationMs: meta.DurationMs, CostUSD: meta.CostUSD}, start), nil
	default:
		return ToolCallResponse{}, fmt.Errorf("tools: tool %q type %q not supported by MVP registry (native|mock|mcp|http)", toolName, tr.Spec.Type)
	}
}
