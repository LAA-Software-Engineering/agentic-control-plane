package spec

// Iteration bound for an agent's reason→act→observe loop (issue #160). Unset or zero uses the
// default; a value above the hard cap is clamped down to it. This is the single source of truth
// for the bound: the internal engine loop (internal/engine) and the external-runtime turn mapping
// (internal/runtime/claudecode → --max-turns, issue #340) both resolve through ResolveMaxIterations
// so a program's iteration ceiling is identical regardless of which runtime executes it.
const (
	DefaultAgentMaxIterations = 8
	HardAgentMaxIterations    = 32
)

// ResolveMaxIterations returns the effective iteration bound for the given agent constraints,
// applying the default and hard cap. A nil constraints block (or an unset/zero maxIterations)
// resolves to DefaultAgentMaxIterations; any value above HardAgentMaxIterations is clamped.
func ResolveMaxIterations(c *AgentConstraints) int {
	n := DefaultAgentMaxIterations
	if c != nil && c.MaxIterations > 0 {
		n = c.MaxIterations
	}
	if n > HardAgentMaxIterations {
		return HardAgentMaxIterations
	}
	return n
}
