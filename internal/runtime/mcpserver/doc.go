// Package mcpserver compiles an agent's capability grants into a per-run Terfyn MCP
// server (epic #335, issue #338). It is the boundary that lets an external CLI agent
// (`claude -p`, driven by internal/runtime/claudecode) act only through the operations
// the grant allows.
//
// The compiler is closed-world by construction: [Compile] enumerates an agent's
// `grants { tool.<name>.<op> }`, resolves each against the deployed/pinned capability
// manifest (issue #204, tools.ManifestFor — never a live tools/list), and produces exactly
// one MCP tool per granted operation. An ungranted operation simply never appears in
// `tools/list`, so it is unreachable rather than denied-after-the-fact.
//
// [Server] serves that set over JSON-RPC (MCP stdio framing: newline-delimited JSON-RPC
// 2.0). Every `tools/call` is routed through a [Dispatcher]; the production
// [PolicyDispatcher] runs the same inner path as Terfyn's own engine loop —
// policy CheckToolCall (which enforces the closed-world manifest and approvals) then
// tools.ToolExecutor.Call — so the capability grant, not the harness prompt, stays the
// authority boundary. Interactive HITL prompting for an out-of-process agent, budget
// mapping (#340), and trace integration (#341) wrap this server in their own issues.
package mcpserver
