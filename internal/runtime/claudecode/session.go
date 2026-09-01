package claudecode

import (
	"context"
	"encoding/json"
)

// AgentRuntime drives an external CLI agent non-interactively: spawn it, stream its structured
// output, and collect a result. It is deliberately distinct from runtime.Runtime — that interface
// drives Terfyn's own engine loop, this one drives an *external* process while Terfyn keeps
// authority, budget, HITL, snapshot, resume, and audit around it (epic #335). Modeling it as its own
// small interface keeps ClaudeCodeRuntime testable with a fake process and lets a future
// CodexRuntime / GeminiCLIRuntime satisfy the same contract.
type AgentRuntime interface {
	RunSession(ctx context.Context, spec RunSpec) (Session, error)
}

// RunSpec is one external agent invocation. Prompt is the user turn; SystemPrompt carries the
// agent's instructions; MCPConfig points at the per-run Terfyn MCP server that advertises exactly
// the granted operations (generated in #338 — empty here means the agent gets no tools). The exact
// CLI flags used to realize this are an implementation detail behind the adapter.
type RunSpec struct {
	Prompt       string
	SystemPrompt string
	MaxTurns     int    // → --max-turns (0 = leave to the CLI default); budget mapping is #340
	MCPConfig    string // → --mcp-config (per-run server, #338)
	ExtraArgs    []string
}

// StopReason is the normalized reason an external session ended.
type StopReason string

const (
	StopSuccess  StopReason = "success"
	StopMaxTurns StopReason = "max_turns"
	StopError    StopReason = "error"
)

// ToolUse is a tool the external agent selected. Under the epic's design every such call is served
// by the per-run MCP server and still passes Terfyn policy / CheckToolCall / HITL (#338); the
// adapter only records that the selection happened.
type ToolUse struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// Turn is one assistant message: its text plus any tool selections in it.
type Turn struct {
	Text     string
	ToolUses []ToolUse
}

// Session is the collected result of an external run, parsed from the CLI's stream-json output.
type Session struct {
	SessionID       string
	Model           string
	AdvertisedTools []string // tools the harness reported at init (should match the granted set)
	Turns           []Turn
	ToolUses        []ToolUse // flattened across turns, in order
	FinalText       string
	NumTurns        int
	CostUSD         float64
	StopReason      StopReason
	IsError         bool
}
