package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/terfyn/internal/models"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/tools"
)

var defaultAgentToolParameters = json.RawMessage(`{"type":"object","properties":{}}`)

func (e *Executor) advertisedAgentTools(agent *spec.AgentResource) (defs []models.ToolDef, usesByName map[string]string, err error) {
	if agent == nil || len(agent.Spec.Tools) == 0 {
		return nil, nil, nil
	}
	if e == nil || e.Graph == nil {
		return nil, nil, fmt.Errorf("engine: agent %q declares tools but the project graph has none", agent.Metadata.Name)
	}
	advertised, err := spec.ResolveAgentAdvertisedTools(agent, e.Graph.Tools)
	if err != nil {
		return nil, nil, fmt.Errorf("engine: %w", err)
	}
	defs = make([]models.ToolDef, 0, len(advertised))
	usesByName = make(map[string]string, len(advertised))
	for _, item := range advertised {
		usesByName[item.Name] = item.Uses
		desc := "Project tool " + item.Name
		if tr := e.Graph.Tools[item.Name]; tr != nil {
			if typ := strings.TrimSpace(tr.Spec.Type); typ != "" {
				desc = "Project tool " + item.Name + " (" + typ + ")"
			}
		}
		defs = append(defs, models.ToolDef{
			Name:        item.Name,
			Description: desc,
			Parameters:  defaultAgentToolParameters,
		})
	}
	return defs, usesByName, nil
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
