// Package engine orchestrates workflow execution, steps, and interpolation.
//
// [InterpolateString] and [InterpolateWalk] implement ${input.*} and ${steps.*} dot paths only (design doc section 13.1 MVP).
// Whole-field tokens keep native types; tokens embedded in surrounding text stringify (issue #193).
//
// [Executor.Run] executes workflow DAGs: interpolated step inputs, policy checks from the
// workflow's Policy resource, tool and agent steps, optional JSON Schema validation for agent output,
// persisted run_steps rows, and trace events (design doc sections 12.2 E, 13.3, 13.4, 14.2).
// Workflows with no `needs:` keep implicit sequential YAML order. Independent `needs:` roots
// run concurrently with [DefaultMaxConcurrentSteps]; join steps see all upstream `${steps.*}`
// outputs. Checkpoints store a completion set so resume can continue a partially finished
// parallel group (issue #192). A `workflow:` step invokes another Workflow in the project graph
// (issue #194); callee `output.value` becomes the step output. Nested progress is stored on the
// checkpoint so resume can continue mid-subworkflow. Trace events stamp `logicalOrder` (YAML index)
// and nested `callStack` for deterministic replay; the audit chain still verifies insert order.
//
// Agent steps with declared tools run a bounded Generate loop (issue #160): the engine attaches
// one [models.ToolDef] per listed Tool (`ToolChoice: auto`). `spec.tools` may name a Tool or pin
// `tool.<name>.<operation>`. Native names advertise `echo`; mock/mcp advertise `default`; HTTP
// requires a pinned `method.path` (including rejecting `tool.<name>.default`). [spec.ValidateProjectGraph]
// applies the same advertised-uses rules. Only that ToolDef name is accepted — aliased ops such as
// `helper.echo` or `helper.command.run` fail before [policy.PolicyEvaluator.CheckToolCall] /
// [tools.ToolExecutor.Call]. Inner calls use the agent `timeoutSeconds` context. After each Generate
// and tool turn, [policy.PolicyEvaluator.CheckRun] re-checks cost and wall-clock budgets
// against already-accumulated run cost before the next Generate or inner tool call, and again
// after each call's actual cost. Exceeding `execution.maxTotalCostUsd` records `limit_hit`
// (`kind: max_cost`) plus `system_error`, then fails the step with `run_error` (issue #163).
// `constraints.maxIterations` (default 8, hard cap 32) counts Generate turns; `tool_use` on the last
// turn fails without executing those calls. HITL interrupt does not run inside the loop: inner uses
// must be pre-approved (`--approve` / ApprovedActions) or CheckToolCall fails closed. Agents with no
// tools remain a single completion. Inner tool calls share workflow-step tracing: `tool_selection`
// (tool name + arguments digest, not raw args) then `tool_execution` (duration, cost, success,
// and a stable `tool_call_failed` reason — not the raw Error() string) after the tool returns,
// including Call failures (issue #161). Policy denial still records
// `system_error` without invoking the tool.
package engine
