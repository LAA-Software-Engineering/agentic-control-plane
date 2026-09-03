package engine

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Terfyn/terfyn/internal/models"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/tools"
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
		// The tool-def name may be a per-operation handle (`workspace.read_file`,
		// #291). Providers require tool names to match ^[A-Za-z0-9_-]{1,128}$ — the
		// '.' in a multi-operation handle is rejected by Anthropic and OpenAI — so the
		// model-facing handle is sanitized. The canonical `uses` string is unchanged,
		// so policy, dispatch, and traces still key off the real operation.
		handle := sanitizeToolDefName(item.Name)
		for i := 2; ; i++ {
			if _, clash := usesByName[handle]; !clash {
				break
			}
			handle = sanitizeToolDefName(item.Name) + "_" + strconv.Itoa(i)
		}
		usesByName[handle] = item.Uses
		desc := "Project tool " + item.Name
		// Resolve the backing Tool by the name parsed from the uses string rather than
		// by the tool-def name, to keep the type in the description.
		toolName := item.Name
		if tn, _, err := tools.ParseUses(item.Uses); err == nil {
			toolName = tn
		}
		if tr := e.Graph.Tools[toolName]; tr != nil {
			if typ := strings.TrimSpace(tr.Spec.Type); typ != "" {
				desc = "Project tool " + item.Name + " (" + typ + ")"
			}
		}
		defs = append(defs, models.ToolDef{
			Name:        handle,
			Description: desc,
			Parameters:  defaultAgentToolParameters,
		})
	}
	return defs, usesByName, nil
}

// sanitizeToolDefName maps a tool-def handle to the provider tool-name pattern
// ^[A-Za-z0-9_-]{1,128}$. Every character outside that set (notably the '.' in a
// per-operation handle like "workspace.read_file", #291) becomes '_'. This renames
// only the handle the model calls by; the canonical uses string is untouched.
func sanitizeToolDefName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	s := b.String()
	if s == "" {
		s = "tool"
	}
	if len(s) > 128 {
		s = s[:128]
	}
	return s
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
