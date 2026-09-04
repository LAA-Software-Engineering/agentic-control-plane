package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Terfyn/terfyn/internal/schema"
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

// ValidateInputSchema validates a tool call's input against the operation's declared JSON Schema
// (#204), resolving the ref against the registry's ProjectRoot. Absent tool/operation/schema means
// gradual (any input), matching the engine's validateToolInputSchema. This lets the external-runtime
// dispatch path enforce the same per-call contract the internal engine does (#390), since both run
// through a *Registry that already holds the graph and project root.
func (r *Registry) ValidateInputSchema(uses string, with map[string]any) error {
	if r == nil || r.graph == nil {
		return nil
	}
	toolName, operation, err := ParseUses(uses)
	if err != nil {
		return nil // malformed uses is handled by policy/dispatch, not this concern.
	}
	tr := r.graph.Tools[toolName]
	if tr == nil {
		return nil
	}
	op, ok := tr.Spec.Operations[operation]
	if !ok || strings.TrimSpace(op.Schema) == "" {
		return nil
	}
	raw, err := json.Marshal(with)
	if err != nil {
		return fmt.Errorf("tools: marshal tool input: %w", err)
	}
	path, err := schema.ResolveSchemaPath(r.ProjectRoot, op.Schema)
	if err != nil {
		return err
	}
	if err := schema.Validate(path, raw); err != nil {
		return fmt.Errorf("tools: tool %q input: %w", uses, err)
	}
	return nil
}

// ResolveToolExecutionLimits resolves the byte/policy limits for a tool call from the project and the
// tool spec (no workflow scope — the external runtime is agent-driven), the same source of truth the
// engine uses. Callers on the external path enforce these to close the #117 gap (#390).
func (r *Registry) ResolveToolExecutionLimits(uses string) spec.ResolvedExecutionLimits {
	var project *spec.ProjectSpec
	var toolSpec *spec.ToolSpec
	if r != nil && r.graph != nil {
		project = &r.graph.Spec
		if tn, ok := spec.ParseToolUses(uses); ok {
			if tr := r.graph.Tools[tn]; tr != nil {
				toolSpec = &tr.Spec
			}
		}
	}
	return spec.ResolveExecutionLimits(project, nil, toolSpec)
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
