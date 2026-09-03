// Package agentcli is the CLI-agnostic kernel shared by every external agent-runtime target
// (epic #335). An external target runs a reviewed .agent program on a third-party agent CLI while
// Terfyn keeps authority, budget, HITL, snapshot, and audit around it. Only the per-CLI driver
// differs; this package holds everything that does not:
//
//   - AgentRuntime — the one seam an adapter implements (spawn the CLI, parse its output into a
//     Session). See internal/runtime/claudecode for the Claude Code driver, and
//     docs/RUNTIME_TARGET_CONTRACT.md for the bar a new driver must meet.
//   - RunSpec / Session / Turn / ToolUse / StopReason — the driver's input and normalized output.
//   - Limits / MapLimits / EnforceBudget — Terfyn-derived bounds mapped onto the harness as a belt,
//     with CheckRun the enforcer of record.
//   - EmitSessionTurns / EmitLimitHit — folding a session into the hash-linked audit chain.
//   - ExternalAgentRun / RunExternalAgent — the composition: grant -> per-run MCP server ->
//     driver.RunSession -> trace -> budget, identical whichever CLI runs the program.
package agentcli
