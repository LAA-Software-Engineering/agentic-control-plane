// Package engine orchestrates workflow execution, steps, and interpolation.
//
// [InterpolateString] and [InterpolateWalk] implement ${input.*} and ${steps.*} dot paths only (design doc section 13.1 MVP).
//
// [Executor.Run] executes sequential workflows: interpolated step inputs, policy checks from the
// workflow's Policy resource, tool and agent steps, optional JSON Schema validation for agent output,
// persisted run_steps rows, and trace events (design doc sections 12.2 E, 13.3, 13.4, 14.2).
//
// Agent steps with declared tools run a bounded Generate loop (issue #160): the engine attaches
// [models.ToolDef]s (`ToolChoice: auto`), executes `tool_use` only after [policy.PolicyEvaluator.CheckToolCall],
// appends `tool_result` turns, and stops on `end_turn` or `constraints.maxIterations` (default 8,
// hard cap 32). Agents with no tools remain a single completion.
package engine
