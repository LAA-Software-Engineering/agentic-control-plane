package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools/mcp"
)

// MCPDiscoveryPerToolTimeout bounds tools/list for one MCP Tool resource during config resolution.
const MCPDiscoveryPerToolTimeout = 10 * time.Second

// MCPDiscoveryWarning reports a non-fatal MCP tools/list failure during safety discovery.
type MCPDiscoveryWarning struct {
	Tool    string `json:"tool"`
	Message string `json:"message"`
}

// ApplyMCPSafetyDiscovery lists MCP tools for each Tool with type mcp, merges
// meta.mcp_flags into spec.safety (author-set fields win), and mutates g in place.
// Discovery failures for individual tools are reported as warnings; fail-closed defaults apply.
func ApplyMCPSafetyDiscovery(ctx context.Context, g *spec.ProjectGraph) []MCPDiscoveryWarning {
	if g == nil || g.Tools == nil {
		return nil
	}
	var warnings []MCPDiscoveryWarning
	for name, tr := range g.Tools {
		if tr == nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(tr.Spec.Type)) != "mcp" || tr.Spec.MCP == nil {
			continue
		}
		toolName := strings.TrimSpace(name)
		if toolName == "" {
			toolName = strings.TrimSpace(tr.Metadata.Name)
		}
		toolCtx, cancel := context.WithTimeout(ctx, MCPDiscoveryPerToolTimeout)
		descriptors, err := mcp.ListTools(toolCtx, tr.Spec.MCP)
		cancel()
		if err != nil {
			warnings = append(warnings, MCPDiscoveryWarning{
				Tool: toolName,
				Message: fmt.Sprintf(
					"MCP tools/list failed for tool %q: %v (using fail-closed safety defaults; pin spec.safety in YAML for stable plan→run digests)",
					toolName,
					err,
				),
			})
			continue
		}
		mcpSafety := mcp.SafetyFromDescriptors(descriptors)
		if mcpSafety == nil && tr.Spec.Safety == nil {
			continue
		}
		tr.Spec.Safety = spec.MergeToolSafety(tr.Spec.Safety, mcpSafety)
	}
	return warnings
}

// FormatMCPDiscoveryWarning renders a warning for validate/plan output.
func FormatMCPDiscoveryWarning(w MCPDiscoveryWarning) string {
	if strings.TrimSpace(w.Message) != "" {
		return w.Message
	}
	if strings.TrimSpace(w.Tool) == "" {
		return "MCP tools/list failed (using fail-closed safety defaults)"
	}
	return fmt.Sprintf("MCP tools/list failed for tool %q (using fail-closed safety defaults)", w.Tool)
}

// IsMCPDiscoveryTimeout reports whether err is a per-tool discovery timeout.
func IsMCPDiscoveryTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}
