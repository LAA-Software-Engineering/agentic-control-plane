package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
)

// defaultAgentToolOperation is used when the tool type has no native single-op mapping (mock/mcp).
const defaultAgentToolOperation = "default"

// nativeAgentToolOperation is the MVP advertised op for type=native (Dispatch always has echo).
const nativeAgentToolOperation = "echo"

var defaultAgentToolParameters = json.RawMessage(`{"type":"object","properties":{}}`)

func (e *Executor) advertisedAgentTools(agent *spec.AgentResource) (defs []models.ToolDef, usesByName map[string]string, err error) {
	if agent == nil || len(agent.Spec.Tools) == 0 {
		return nil, nil, nil
	}
	if e == nil || e.Graph == nil || e.Graph.Tools == nil {
		return nil, nil, fmt.Errorf("engine: agent %q declares tools but the project graph has none", agent.Metadata.Name)
	}
	defs = make([]models.ToolDef, 0, len(agent.Spec.Tools))
	usesByName = make(map[string]string, len(agent.Spec.Tools))
	for _, raw := range agent.Spec.Tools {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		name, pinned, err := parseAgentToolEntry(entry)
		if err != nil {
			return nil, nil, fmt.Errorf("engine: agent %q: %w", agent.Metadata.Name, err)
		}
		tr, ok := e.Graph.Tools[name]
		if !ok || tr == nil {
			return nil, nil, fmt.Errorf("engine: agent %q declares unknown tool %q", agent.Metadata.Name, name)
		}
		uses := pinned
		if uses == "" {
			uses, err = advertisedAgentUses(name, tr)
			if err != nil {
				return nil, nil, fmt.Errorf("engine: agent %q: %w", agent.Metadata.Name, err)
			}
		}
		if prev, dup := usesByName[name]; dup {
			if prev == uses {
				continue
			}
			return nil, nil, fmt.Errorf("engine: agent %q declares tool %q twice with different operations (%s vs %s)", agent.Metadata.Name, name, prev, uses)
		}
		usesByName[name] = uses
		desc := strings.TrimSpace(tr.Spec.Type)
		if desc != "" {
			desc = "Project tool " + name + " (" + desc + ")"
		} else {
			desc = "Project tool " + name
		}
		defs = append(defs, models.ToolDef{
			Name:        name,
			Description: desc,
			Parameters:  defaultAgentToolParameters,
		})
	}
	return defs, usesByName, nil
}

// parseAgentToolEntry accepts a Tool metadata name or a pinned uses string
// tool.<name>.<operation>. Bare tool.<name> (no operation) is treated as the name.
func parseAgentToolEntry(entry string) (toolName, pinnedUses string, err error) {
	if tn, op, perr := tools.ParseUses(entry); perr == nil {
		return tn, "tool." + tn + "." + op, nil
	}
	if strings.HasPrefix(entry, "tool.") {
		if name, ok := spec.ParseToolUses(entry); ok {
			return name, "", nil
		}
		return "", "", fmt.Errorf("invalid tool entry %q (want <name> or tool.<name>.<operation>)", entry)
	}
	return entry, "", nil
}

func advertisedAgentOperation(toolName string, tr *spec.ToolResource) (string, error) {
	if tr == nil {
		return defaultAgentToolOperation, nil
	}
	switch strings.ToLower(strings.TrimSpace(tr.Spec.Type)) {
	case "native":
		// Native catalog ops are named (echo, identity, command.run, …). Agents
		// that list the tool by metadata name get echo only — never command.run.
		return nativeAgentToolOperation, nil
	case "http":
		// httptool.parseOperation maps an unknown op to GET /<op>, so "default"
		// would become GET /default. Require an explicit method.path in YAML.
		return "", fmt.Errorf("tool %q is type http; list tool.%s.<method.path> in spec.tools (HTTP has no default operation)", toolName, toolName)
	default:
		return defaultAgentToolOperation, nil
	}
}

func advertisedAgentUses(toolName string, tr *spec.ToolResource) (string, error) {
	op, err := advertisedAgentOperation(toolName, tr)
	if err != nil {
		return "", err
	}
	return "tool." + toolName + "." + op, nil
}

// resolveAgentToolCall maps a model tool name onto the single advertised uses string.
// Only the ToolDef name given to the model is accepted — not an arbitrary operation on
// that tool (ADR 002: no operation is agent-callable unless it was advertised).
func resolveAgentToolCall(name string, advertised map[string]string) (uses string, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("engine: tool call missing name")
	}
	uses, ok := advertised[name]
	if !ok || uses == "" {
		return "", fmt.Errorf("engine: tool %q is not declared on the agent", name)
	}
	return uses, nil
}

func parseToolCallArgs(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("engine: tool call arguments are not JSON: %w", err)
	}
	if v == nil {
		return map[string]any{}, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("engine: tool call arguments must be a JSON object")
	}
	return m, nil
}

func encodeToolResultContent(out map[string]any) string {
	if out == nil {
		return "{}"
	}
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Sprintf("%v", out)
	}
	return string(b)
}

func addGenerateMeta(dst *models.GenerateMeta, src models.GenerateMeta) {
	if dst == nil {
		return
	}
	dst.DurationMs += src.DurationMs
	dst.PromptTokens += src.PromptTokens
	dst.CompletionTokens += src.CompletionTokens
	dst.CostUSD += src.CostUSD
}

func addToolMeta(dst *models.GenerateMeta, src tools.ToolCallMeta) {
	if dst == nil {
		return
	}
	dst.DurationMs += src.DurationMs
	dst.CostUSD += src.CostUSD
}
