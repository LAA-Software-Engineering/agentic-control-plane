package tools

import (
	"context"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools/mcp"
)

// ApplyMCPSafetyDiscovery lists MCP tools for each Tool with type mcp, merges
// meta.mcp_flags into spec.safety (author-set fields win), and mutates g in place.
// Discovery failures for individual tools are ignored so validate/plan remain usable offline.
func ApplyMCPSafetyDiscovery(ctx context.Context, g *spec.ProjectGraph) {
	if g == nil || g.Tools == nil {
		return
	}
	for _, tr := range g.Tools {
		if tr == nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(tr.Spec.Type)) != "mcp" || tr.Spec.MCP == nil {
			continue
		}
		descriptors, err := mcp.ListTools(ctx, tr.Spec.MCP)
		if err != nil {
			continue
		}
		mcpSafety := mcp.SafetyFromDescriptors(descriptors)
		if mcpSafety == nil && tr.Spec.Safety == nil {
			continue
		}
		tr.Spec.Safety = spec.MergeToolSafety(tr.Spec.Safety, mcpSafety)
	}
}
