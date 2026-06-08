package spec

// MCPMetaFlagsKey is the MCP tool descriptor meta key for safety flags (issue #125).
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

// MergeMCPToolSafetyFlags combines safety parsed from multiple MCP tool descriptors on one
// server. The merge is conservative (fail-closed): any untrusted, side-effecting, or
// approval-required descriptor makes the aggregate restrictive for that dimension.
// Returns nil when no recognized flags are present in any descriptor.
func MergeMCPToolSafetyFlags(flags ...*ToolSafety) *ToolSafety {
	var parts []*ToolSafety
	for _, f := range flags {
		if f != nil {
			parts = append(parts, f)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	out := &ToolSafety{}
	if v, ok := mergeBoolFieldConservative(parts, fieldTrusted); ok {
		out.Trusted = &v
	}
	if v, ok := mergeBoolFieldConservative(parts, fieldSideEffects); ok {
		out.SideEffects = &v
	}
	if v, ok := mergeBoolFieldConservative(parts, fieldRequiresApproval); ok {
		out.RequiresApproval = &v
	}
	if out.Trusted == nil && out.SideEffects == nil && out.RequiresApproval == nil {
		return nil
	}
	return out
}

type safetyField int

const (
	fieldTrusted safetyField = iota
	fieldSideEffects
	fieldRequiresApproval
)

func mergeBoolFieldConservative(parts []*ToolSafety, field safetyField) (bool, bool) {
	setCount := 0
	for _, p := range parts {
		ptr := safetyFieldPtr(p, field)
		if ptr == nil {
			continue
		}
		setCount++
		if restrictiveBool(*ptr, field) {
			return restrictiveValue(field), true
		}
	}
	if setCount == 0 {
		return false, false
	}
	if setCount == len(parts) {
		return permissiveValue(field), true
	}
	return false, false
}

func safetyFieldPtr(s *ToolSafety, field safetyField) *bool {
	switch field {
	case fieldTrusted:
		return s.Trusted
	case fieldSideEffects:
		return s.SideEffects
	case fieldRequiresApproval:
		return s.RequiresApproval
	default:
		return nil
	}
}

func restrictiveBool(v bool, field safetyField) bool {
	switch field {
	case fieldTrusted:
		return !v
	case fieldSideEffects, fieldRequiresApproval:
		return v
	default:
		return false
	}
}

func restrictiveValue(field safetyField) bool {
	switch field {
	case fieldTrusted:
		return false
	case fieldSideEffects, fieldRequiresApproval:
		return true
	default:
		return false
	}
}

func permissiveValue(field safetyField) bool {
	switch field {
	case fieldTrusted:
		return true
	case fieldSideEffects, fieldRequiresApproval:
		return false
	default:
		return false
	}
}

// SafetyFromMCPMeta maps MCP tool descriptor meta[MCPMetaFlagsKey] onto [ToolSafety].
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
