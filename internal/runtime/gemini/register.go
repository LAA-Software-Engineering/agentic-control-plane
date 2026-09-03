// Package gemini is the external Gemini CLI agent-runtime target (epic #409): a reviewed .agent
// program driven by `gemini -p`, fenced to exactly the per-run Terfyn MCP tools by an isolated
// workspace settings.json (empty tools.core + a single mcpServers entry). It is the Claude runtime's
// sibling — only the driver (argv + settings + output parsing) differs; the CLI-agnostic run
// orchestration is shared via agentcli.RuntimeAdapter.
//
// The Gemini contract is UNVERIFIED against a pinned binary (see gemini.go); the gated live S9 test
// is the gate before it serves a live run.
package gemini

import (
	"github.com/Terfyn/terfyn/internal/runtime"
	"github.com/Terfyn/terfyn/internal/runtime/agentcli"
)

func init() {
	runtime.Register(Name, NewFromDeps)
}

// NewFromDeps wires the shared agentcli adapter with the Gemini driver. Registered as the "gemini"
// runtime factory.
func NewFromDeps(deps runtime.Deps) (runtime.Runtime, error) {
	return agentcli.NewRuntimeAdapter(Name, GeminiRuntime{}, deps), nil
}
