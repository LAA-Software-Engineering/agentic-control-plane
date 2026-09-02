package mcpserver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/tools"
)

// GrantedOp is one operation the grant compiled into the per-run MCP surface: the MCP
// tool the external agent sees, tied back to the canonical `uses` string Terfyn enforces.
type GrantedOp struct {
	// MCPName is the tools/list name the external agent calls, derived from Uses so it is a
	// valid MCP tool identifier: "tool.workspace.read_file" -> "workspace_read_file".
	MCPName string
	// Uses is the canonical grant string ("tool.<name>.<operation>"), the vocabulary the
	// policy path (CheckToolCall) and tools.Registry.Call speak.
	Uses      string
	Tool      string
	Operation string
	// Effects are the operation's declared effects from the pinned manifest (may be empty).
	Effects []string
}

// CompiledServer is the closed callable world for one agent: exactly the granted operations,
// in a stable order, ready to be served by a [Server].
type CompiledServer struct {
	Agent string
	Ops   []GrantedOp
}

// Compile turns an agent's grants into the closed-world MCP tool set.
//
// The source of truth is the deployed/pinned capability manifest (tools.ManifestFor, issue
// #204) — never a live tools/list. A grant whose operation is not a member of a *closed*
// manifest is rejected here (defense in depth: validate already enforces this, but the
// per-run server must never advertise an operation the closed world does not contain). A
// grant on a tool with an open manifest (no declared operations) is passed through, matching
// the closed-world semantics elsewhere: open tools opt out of the manifest bound.
//
// Duplicate grants collapse to one MCP tool; two distinct grants that would collide on a
// single MCPName are an error rather than a silently dropped capability.
func Compile(graph *spec.ProjectGraph, agentName string) (CompiledServer, error) {
	cs := CompiledServer{Agent: agentName}
	if graph == nil || graph.Agents == nil {
		return cs, fmt.Errorf("mcpserver: no agent %q in graph", agentName)
	}
	ar, ok := graph.Agents[agentName]
	if !ok || ar == nil {
		return cs, fmt.Errorf("mcpserver: no agent %q in graph", agentName)
	}

	byName := make(map[string]GrantedOp)
	byUses := make(map[string]bool)
	for _, uses := range ar.Spec.Tools {
		uses = strings.TrimSpace(uses)
		if uses == "" || byUses[uses] {
			continue // ignore blanks and collapse duplicate grants
		}
		byUses[uses] = true

		toolName, operation, err := tools.ParseUses(uses)
		if err != nil {
			return CompiledServer{}, fmt.Errorf("mcpserver: agent %q grant %q: %w", agentName, uses, err)
		}
		m := tools.ManifestFor(graph, toolName)
		if !m.Allows(operation) {
			return CompiledServer{}, fmt.Errorf("mcpserver: agent %q grants %q but operation %q is not in the closed capability manifest for tool %q", agentName, uses, operation, toolName)
		}

		op := GrantedOp{
			MCPName:   mcpToolName(toolName, operation),
			Uses:      uses,
			Tool:      toolName,
			Operation: operation,
			Effects:   manifestEffects(m, operation),
		}
		if prev, clash := byName[op.MCPName]; clash {
			return CompiledServer{}, fmt.Errorf("mcpserver: agent %q grants %q and %q collide on MCP tool name %q", agentName, prev.Uses, op.Uses, op.MCPName)
		}
		byName[op.MCPName] = op
	}

	cs.Ops = make([]GrantedOp, 0, len(byName))
	for _, op := range byName {
		cs.Ops = append(cs.Ops, op)
	}
	sort.Slice(cs.Ops, func(i, j int) bool { return cs.Ops[i].MCPName < cs.Ops[j].MCPName })
	return cs, nil
}

// mcpToolName derives a valid MCP tool identifier from a tool name and (possibly dotted)
// operation: the dots that separate operation segments become underscores, so
// tool "github", operation "pull_request.post_comment" -> "github_pull_request_post_comment".
func mcpToolName(tool, operation string) string {
	return tool + "_" + strings.ReplaceAll(operation, ".", "_")
}

func manifestEffects(m tools.CapabilityManifest, operation string) []string {
	for _, mo := range m.Operations {
		if mo.Name == operation {
			return mo.Effects
		}
	}
	return nil
}
