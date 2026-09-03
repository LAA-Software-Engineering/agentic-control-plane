// Package claudecode is the external Claude Code agent-runtime target (epic #335): a reviewed
// .agent program driven by an external CLI agent (`claude -p`) that sees only the operations the
// grant compiled into a per-run Terfyn MCP server — every call still passes Terfyn policy /
// CheckToolCall / HITL, so the capability grant stays the boundary, not the harness prompt.
//
// The package is now just the Claude-specific driver: argv construction and stream-json parsing
// (claude.go, stream.go) and the S9 flag guards (soundness.go), implementing agentcli.AgentRuntime.
// The CLI-agnostic run orchestration — resolve the driven agent, stand up the per-run MCP server,
// spawn, trace, budget — lives in internal/runtime/agentcli and is shared with every other external
// runtime (agentcli.RuntimeAdapter).
package claudecode

import (
	"github.com/Terfyn/terfyn/internal/runtime"
	"github.com/Terfyn/terfyn/internal/runtime/agentcli"
)

// Name is the --runtime selector and RuntimeTarget name for the external Claude Code runtime.
const Name = "claude-code"

// NewFromDeps wires the shared agentcli adapter with the Claude Code driver. Registered as the
// "claude-code" runtime factory.
func NewFromDeps(deps runtime.Deps) (runtime.Runtime, error) {
	return agentcli.NewRuntimeAdapter(Name, ClaudeCodeRuntime{}, deps), nil
}
