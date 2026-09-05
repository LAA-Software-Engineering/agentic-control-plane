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

// DefaultAgentMaxTokens is the per-completion output-token cap when constraints.maxTokens is unset
// (issue #514). It replaces the old hardcoded 4096, which was a chat-era default that truncated any
// agent writing real content (a coding agent's whole-file write_file exceeds it). 16384 is a
// realistic agent default that still stays under non-streaming HTTP timeouts; an author raises it
// per agent with constraints.maxTokens for larger outputs. There is no hard clamp — a value the
// provider cannot honor is rejected loudly at request time rather than silently capped here.
const DefaultAgentMaxTokens = 16384

// ResolveMaxTokens returns the effective output-token cap for the given agent constraints. A nil
// constraints block (or an unset/zero maxTokens) resolves to DefaultAgentMaxTokens; any positive
// value is used verbatim.
func ResolveMaxTokens(c *AgentConstraints) int {
	if c != nil && c.MaxTokens > 0 {
		return c.MaxTokens
	}
	return DefaultAgentMaxTokens
}
