// Package engine orchestrates workflow execution, steps, and interpolation.
//
// [InterpolateString] and [InterpolateWalk] implement ${input.*} and ${steps.*} dot paths only (design doc section 13.1 MVP).
//
// [Executor.Run] executes sequential workflows: interpolated step inputs, policy checks from the
// workflow's Policy resource, tool and agent steps, optional JSON Schema validation for agent output,
// persisted run_steps rows, and trace events (design doc sections 12.2 E, 13.3, 13.4, 14.2).
//
// Agent steps with declared tools run a bounded Generate loop (issue #160): the engine attaches
// one [models.ToolDef] per listed Tool (`ToolChoice: auto`). `spec.tools` may name a Tool or pin
// `tool.<name>.<operation>`. Native names advertise `echo`; mock/mcp advertise `default`; HTTP
// requires a pinned uses string. Only that ToolDef name is accepted — aliased ops such as
// `helper.echo` or `helper.command.run` fail before [policy.PolicyEvaluator.CheckToolCall] /
// [tools.ToolExecutor.Call]. `constraints.maxIterations` (default 8, hard cap 32) counts Generate
// turns; `tool_use` on the last turn fails without executing those calls. HITL interrupt does not
// run inside the loop: inner uses must be pre-approved (`--approve` / ApprovedActions) or
// CheckToolCall fails closed. Agents with no tools remain a single completion.
package engine
