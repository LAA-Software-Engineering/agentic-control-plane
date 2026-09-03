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

// ResolveAgentAdvertisedTools maps agent.spec.tools onto the operations advertised
// to an agent loop. Entries may be a Tool metadata name or a pinned uses string
// tool.<name>.<operation>. Native names advertise echo; mock/mcp advertise default;
// HTTP requires a method.path pin.
//
// A single autonomous Tool grant may expose MULTIPLE operations (issue #291): an
// agent may list tool.workspace.read_file, tool.workspace.write_file, and
// tool.workspace.run_tests on the one `workspace` tool. Each distinct operation
// becomes its own advertised tool-def with its own uses, so the model can address
// them separately and each is gated independently — the capability boundary is per
// operation, not per tool. To stay backward compatible, a tool with a SINGLE
// granted operation keeps the bare tool name as its tool-def name (`workspace`); a
// tool with SEVERAL is disambiguated as `<name>.<operation>`
// (`workspace.read_file`). The resulting handle is normalized with [AgentToolName]
// before it enters the shared model/MCP namespace. An exact-duplicate operation
// listed twice is idempotent.
func ResolveAgentAdvertisedTools(agent *AgentResource, tools map[string]*ToolResource) ([]AdvertisedAgentTool, error) {
	if agent == nil || len(agent.Spec.Tools) == 0 {
		return nil, nil
	}
	agentName := agent.Metadata.Name
	if tools == nil {
		return nil, agent.Pos.Errorf("Agent/%s: declares tools but the project graph has none", agentName)
	}
	type resolvedEntry struct {
		toolName string
		uses     string
	}
	var entries []resolvedEntry
	seenUses := make(map[string]struct{}, len(agent.Spec.Tools))
	opsPerTool := make(map[string]int)
	for i, raw := range agent.Spec.Tools {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		pos := agent.Pos
		if i < len(agent.Spec.ToolsPos) && !agent.Spec.ToolsPos[i].IsZero() {
			pos = agent.Spec.ToolsPos[i]
		}
		name, pinned, err := parseAgentToolEntry(entry)
		if err != nil {
			return nil, pos.Errorf("Agent/%s: %w", agentName, err)
		}
		tr, ok := tools[name]
		if !ok || tr == nil {
			return nil, pos.Errorf("Agent/%s: declares unknown tool %q", agentName, name)
		}
		uses := pinned
		if uses == "" {
			uses, err = advertisedAgentUses(name, tr)
			if err != nil {
				return nil, pos.Errorf("Agent/%s: %w", agentName, err)
			}
		} else if err := validatePinnedAgentUses(name, uses, tr); err != nil {
			return nil, pos.Errorf("Agent/%s: %w", agentName, err)
		}
		if _, dup := seenUses[uses]; dup {
			continue // the same operation listed twice is idempotent
		}
		seenUses[uses] = struct{}{}
		entries = append(entries, resolvedEntry{toolName: name, uses: uses})
		opsPerTool[name]++
	}
	out := make([]AdvertisedAgentTool, 0, len(entries))
	// The normalized provider/MCP name is the callable namespace. Distinct source
	// handles may collide there because punctuation is replaced or because the name
	// is truncated to the provider's 128-character limit. Reject such collisions at
	// resolution so validate and plan fail closed before any runtime is selected.
	seenName := make(map[string]string, len(entries))
	for _, e := range entries {
		defName := e.toolName
		if opsPerTool[e.toolName] > 1 {
			defName = e.toolName + "." + operationFromUses(e.toolName, e.uses)
		}
		defName = AgentToolName(defName)
		if prev, dup := seenName[defName]; dup {
			return nil, agent.Pos.Errorf("Agent/%s: two granted operations map to the same provider tool name %q (%s vs %s); rename the tool or operation to disambiguate", agentName, defName, prev, e.uses)
		}
		seenName[defName] = e.uses
		out = append(out, AdvertisedAgentTool{Name: defName, Uses: e.uses})
	}
	return out, nil
}

// AgentToolName maps an agent tool-def handle to the shared model/MCP tool-name
// pattern ^[A-Za-z0-9_-]{1,128}$. Every character outside that set (notably the
// dots in per-operation handles such as "workspace.read_file") becomes '_'. The
// canonical uses string remains unchanged and is the policy/dispatch identity.
func AgentToolName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
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
