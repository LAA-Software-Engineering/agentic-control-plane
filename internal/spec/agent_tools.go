package spec

import (
	"fmt"
	"strings"
)

const (
	defaultAgentToolOperation = "default"
	nativeAgentToolOperation  = "echo"
)

// AdvertisedAgentTool is one ToolDef-name → uses binding for an agent loop (issue #160).
type AdvertisedAgentTool struct {
	Name string
	Uses string
}

// ResolveAgentAdvertisedTools maps agent.spec.tools onto one uses string per Tool name.
// Entries may be a Tool metadata name or a pinned uses string tool.<name>.<operation>.
// Native names advertise echo; mock/mcp advertise default; HTTP requires a method.path pin.
func ResolveAgentAdvertisedTools(agent *AgentResource, tools map[string]*ToolResource) ([]AdvertisedAgentTool, error) {
	if agent == nil || len(agent.Spec.Tools) == 0 {
		return nil, nil
	}
	agentName := agent.Metadata.Name
	if tools == nil {
		return nil, fmt.Errorf("Agent/%s: declares tools but the project graph has none", agentName)
	}
	var out []AdvertisedAgentTool
	usesByName := make(map[string]string, len(agent.Spec.Tools))
	for _, raw := range agent.Spec.Tools {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		name, pinned, err := parseAgentToolEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("Agent/%s: %w", agentName, err)
		}
		tr, ok := tools[name]
		if !ok || tr == nil {
			return nil, fmt.Errorf("Agent/%s: declares unknown tool %q", agentName, name)
		}
		uses := pinned
		if uses == "" {
			uses, err = advertisedAgentUses(name, tr)
			if err != nil {
				return nil, fmt.Errorf("Agent/%s: %w", agentName, err)
			}
		} else if err := validatePinnedAgentUses(name, uses, tr); err != nil {
			return nil, fmt.Errorf("Agent/%s: %w", agentName, err)
		}
		if prev, dup := usesByName[name]; dup {
			if prev == uses {
				continue
			}
			return nil, fmt.Errorf("Agent/%s: declares tool %q twice with different operations (%s vs %s)", agentName, name, prev, uses)
		}
		usesByName[name] = uses
		out = append(out, AdvertisedAgentTool{Name: name, Uses: uses})
	}
	return out, nil
}

// parseAgentToolEntry accepts a Tool metadata name or a pinned uses string
// tool.<name>.<operation>. Bare tool.<name> (no operation) is treated as the name.
func parseAgentToolEntry(entry string) (toolName, pinnedUses string, err error) {
	entry = strings.TrimSpace(entry)
	if strings.HasPrefix(entry, "tool.") {
		rest := strings.TrimPrefix(entry, "tool.")
		if rest == "" {
			return "", "", fmt.Errorf("invalid tool entry %q (want <name> or tool.<name>.<operation>)", entry)
		}
		i := strings.IndexByte(rest, '.')
		if i < 0 {
			return rest, "", nil
		}
		name := rest[:i]
		op := rest[i+1:]
		if name == "" || strings.TrimSpace(op) == "" {
			return "", "", fmt.Errorf("invalid tool entry %q (want <name> or tool.<name>.<operation>)", entry)
		}
		return name, "tool." + name + "." + op, nil
	}
	return entry, "", nil
}

func advertisedAgentOperation(toolName string, tr *ToolResource) (string, error) {
	if tr == nil {
		return defaultAgentToolOperation, nil
	}
	switch strings.ToLower(strings.TrimSpace(tr.Spec.Type)) {
	case "native":
		return nativeAgentToolOperation, nil
	case "http":
		return "", fmt.Errorf("tool %q is type http; list tool.%s.<method.path> in spec.tools (HTTP has no default operation)", toolName, toolName)
	default:
		return defaultAgentToolOperation, nil
	}
}

func advertisedAgentUses(toolName string, tr *ToolResource) (string, error) {
	op, err := advertisedAgentOperation(toolName, tr)
	if err != nil {
		return "", err
	}
	return "tool." + toolName + "." + op, nil
}

func validatePinnedAgentUses(toolName, uses string, tr *ToolResource) error {
	if tr == nil || strings.ToLower(strings.TrimSpace(tr.Spec.Type)) != "http" {
		return nil
	}
	op := operationFromUses(toolName, uses)
	if !httpOperationIsMethodPath(op) {
		return fmt.Errorf("tool %q is type http; list tool.%s.<method.path> in spec.tools (HTTP has no default operation)", toolName, toolName)
	}
	return nil
}

func operationFromUses(toolName, uses string) string {
	prefix := "tool." + toolName + "."
	if strings.HasPrefix(uses, prefix) {
		return uses[len(prefix):]
	}
	return ""
}

func httpOperationIsMethodPath(op string) bool {
	op = strings.TrimSpace(op)
	if op == "" || strings.EqualFold(op, defaultAgentToolOperation) {
		return false
	}
	verb := op
	if i := strings.IndexByte(op, '.'); i >= 0 {
		verb = op[:i]
	}
	switch strings.ToLower(verb) {
	case "get", "post", "put", "delete", "patch":
		return true
	default:
		return false
	}
}
