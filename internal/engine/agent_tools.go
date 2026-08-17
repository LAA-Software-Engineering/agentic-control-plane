package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
)

// defaultAgentToolOperation is the MVP single operation advertised for each
// agent-declared Tool resource (issue #160). Refine per-transport later.
const defaultAgentToolOperation = "default"

var defaultAgentToolParameters = json.RawMessage(`{"type":"object","properties":{}}`)

func (e *Executor) agentToolDefs(agent *spec.AgentResource) ([]models.ToolDef, error) {
	if agent == nil || len(agent.Spec.Tools) == 0 {
		return nil, nil
	}
	if e == nil || e.Graph == nil || e.Graph.Tools == nil {
		return nil, fmt.Errorf("engine: agent %q declares tools but the project graph has none", agent.Metadata.Name)
	}
	out := make([]models.ToolDef, 0, len(agent.Spec.Tools))
	seen := make(map[string]struct{}, len(agent.Spec.Tools))
	for _, raw := range agent.Spec.Tools {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		tr, ok := e.Graph.Tools[name]
		if !ok || tr == nil {
			return nil, fmt.Errorf("engine: agent %q declares unknown tool %q", agent.Metadata.Name, name)
		}
		desc := strings.TrimSpace(tr.Spec.Type)
		if desc != "" {
			desc = "Project tool " + name + " (" + desc + ")"
		} else {
			desc = "Project tool " + name
		}
		out = append(out, models.ToolDef{
			Name:        name,
			Description: desc,
			Parameters:  defaultAgentToolParameters,
		})
	}
	return out, nil
}

func declaredAgentTools(agent *spec.AgentResource) map[string]struct{} {
	out := map[string]struct{}{}
	if agent == nil {
		return out
	}
	for _, raw := range agent.Spec.Tools {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	return out
}

func defaultAgentToolUses(toolName string) string {
	return "tool." + toolName + "." + defaultAgentToolOperation
}

// resolveAgentToolCall maps a model tool name onto a workflow uses string.
// Accepted forms: "<tool>", "<tool>.<operation>", "tool.<tool>.<operation>".
func resolveAgentToolCall(name string, declared map[string]struct{}) (uses string, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("engine: tool call missing name")
	}
	if tn, ok := spec.ParseToolUses(name); ok {
		if _, allowed := declared[tn]; !allowed {
			return "", fmt.Errorf("engine: tool %q is not declared on the agent", tn)
		}
		if strings.Count(strings.TrimPrefix(name, "tool."), ".") == 0 {
			return defaultAgentToolUses(tn), nil
		}
		return name, nil
	}
	toolName, op, ok := splitToolAndOperation(name)
	if !ok {
		if _, allowed := declared[name]; !allowed {
			return "", fmt.Errorf("engine: tool %q is not declared on the agent", name)
		}
		return defaultAgentToolUses(name), nil
	}
	if _, allowed := declared[toolName]; !allowed {
		return "", fmt.Errorf("engine: tool %q is not declared on the agent", toolName)
	}
	return "tool." + toolName + "." + op, nil
}

func splitToolAndOperation(name string) (toolName, operation string, ok bool) {
	i := strings.IndexByte(name, '.')
	if i <= 0 || i >= len(name)-1 {
		return "", "", false
	}
	return name[:i], name[i+1:], true
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
