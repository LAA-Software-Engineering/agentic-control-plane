package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
)

// defaultAgentToolOperation is used when the tool type has no native single-op mapping (mock/mcp/http).
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
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, dup := usesByName[name]; dup {
			continue
		}
		tr, ok := e.Graph.Tools[name]
		if !ok || tr == nil {
			return nil, nil, fmt.Errorf("engine: agent %q declares unknown tool %q", agent.Metadata.Name, name)
		}
		uses := advertisedAgentUses(name, tr)
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

func advertisedAgentOperation(tr *spec.ToolResource) string {
	if tr == nil {
		return defaultAgentToolOperation
	}
	switch strings.ToLower(strings.TrimSpace(tr.Spec.Type)) {
	case "native":
		// Native catalog ops are named (echo, identity, command.run, …). Agents
		// that list the tool by metadata name get echo only — never command.run.
		return nativeAgentToolOperation
	default:
		// mock/mcp accept a synthetic default op. HTTP interprets "default" as
		// GET /default; prefer a workflow uses step with an explicit method.path.
		return defaultAgentToolOperation
	}
}

func advertisedAgentUses(toolName string, tr *spec.ToolResource) string {
	return "tool." + toolName + "." + advertisedAgentOperation(tr)
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
