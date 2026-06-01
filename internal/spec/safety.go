package spec

// MCP descriptor meta key for tool safety flags (not wired at discovery yet; see CHANGELOG).
const MCPMetaFlagsKey = "mcp_flags"

// Fail-closed defaults for tool safety (issue #103, WayFind-aligned).
const (
	defaultToolTrusted     = false
	defaultToolSideEffects = true
)

// ResolveToolSafety applies fail-closed defaults and derives requiresApproval when unset.
//
// Derivation when requiresApproval is omitted:
//   - trusted → does not require approval
//   - untrusted and no side effects → does not require approval (read-only)
//   - otherwise → requires approval
func ResolveToolSafety(s *ToolSafety) ResolvedToolSafety {
	r := ResolvedToolSafety{
		Trusted:     defaultToolTrusted,
		SideEffects: defaultToolSideEffects,
	}
	if s != nil {
		if s.Trusted != nil {
			r.Trusted = *s.Trusted
		}
		if s.SideEffects != nil {
			r.SideEffects = *s.SideEffects
		}
		if s.RequiresApproval != nil {
			r.RequiresApproval = *s.RequiresApproval
			return r
		}
	}
	r.RequiresApproval = deriveRequiresApproval(r.Trusted, r.SideEffects)
	return r
}

func deriveRequiresApproval(trusted, sideEffects bool) bool {
	if trusted {
		return false
	}
	if !sideEffects {
		return false
	}
	return true
}

// NormalizeToolSafety mutates spec.Safety so resolved bools are materialized for stable plan output.
// Idempotent when called on an already-normalized safety block.
func NormalizeToolSafety(spec *ToolSpec) {
	if spec == nil {
		return
	}
	resolved := ResolveToolSafety(spec.Safety)
	spec.Safety = &ToolSafety{
		Trusted:          BoolPtr(resolved.Trusted),
		SideEffects:      BoolPtr(resolved.SideEffects),
		RequiresApproval: BoolPtr(resolved.RequiresApproval),
	}
}

// BoolPtr returns a pointer to b (for optional YAML bool fields).
func BoolPtr(b bool) *bool {
	v := b
	return &v
}

// MergeToolSafety combines author-set safety with MCP-discovered flags.
// Not called from MCP discovery yet; see CHANGELOG [Unreleased] / issue #103 follow-up.
// Precedence: author (base) wins over MCP for each field that base sets explicitly.
func MergeToolSafety(author, mcp *ToolSafety) *ToolSafety {
	if author == nil && mcp == nil {
		return nil
	}
	out := &ToolSafety{}
	if mcp != nil {
		out.Trusted = mcp.Trusted
		out.SideEffects = mcp.SideEffects
		out.RequiresApproval = mcp.RequiresApproval
	}
	if author != nil {
		if author.Trusted != nil {
			out.Trusted = author.Trusted
		}
		if author.SideEffects != nil {
			out.SideEffects = author.SideEffects
		}
		if author.RequiresApproval != nil {
			out.RequiresApproval = author.RequiresApproval
		}
	}
	if out.Trusted == nil && out.SideEffects == nil && out.RequiresApproval == nil {
		return nil
	}
	return out
}

// SafetyFromMCPMeta maps MCP tool descriptor meta[MCPMetaFlagsKey] onto [ToolSafety].
// Not called from MCP discovery yet; see CHANGELOG [Unreleased] / issue #103 follow-up.
// Returns nil when meta is nil or carries no recognized flags.
func SafetyFromMCPMeta(meta map[string]any) *ToolSafety {
	if meta == nil {
		return nil
	}
	raw, ok := meta[MCPMetaFlagsKey]
	if !ok {
		return nil
	}
	flags, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	var s ToolSafety
	if v, ok := boolFromMeta(flags["trusted"]); ok {
		s.Trusted = &v
	}
	if v, ok := boolFromMeta(flags["side_effects"]); ok {
		s.SideEffects = &v
	}
	if v, ok := boolFromMeta(flags["requires_approval"]); ok {
		s.RequiresApproval = &v
	}
	if s.Trusted == nil && s.SideEffects == nil && s.RequiresApproval == nil {
		return nil
	}
	return &s
}

func boolFromMeta(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	default:
		return false, false
	}
}
