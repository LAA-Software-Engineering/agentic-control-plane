package mcpserver

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/tools"
)

// Dispatcher runs one granted MCP tool call. The Server has already enforced the closed
// world (the uses string corresponds to an advertised grant); the Dispatcher's job is to run
// that call through the rest of Terfyn's authority path and return the tool output.
type Dispatcher interface {
	Call(ctx context.Context, uses string, args map[string]any) (map[string]any, error)
}

// PolicyDispatcher routes each call through the same inner path as Terfyn's own engine loop:
// policy CheckToolCall (which enforces the closed-world manifest, forbidUnknownTools, and
// approvals) and then tools.ToolExecutor.Call. It holds the run-level context so budget and
// approval decisions see the live run state.
//
// Interactive HITL prompting for an out-of-process agent is not resolved here: CheckToolCall
// enforces approvals as policy (an unapproved required action is denied, surfaced to the agent
// as an isError result), and wiring an interactive approval round-trip through the external
// runtime is left to the live runtime integration (#341).
type PolicyDispatcher struct {
	eval policy.PolicyEvaluator
	exec tools.ToolExecutor
	run  policy.RunContext
	seq  atomic.Int64
}

// NewPolicyDispatcher builds a dispatcher bound to a policy evaluator, a tool executor, and
// the run context that approval/budget checks read.
func NewPolicyDispatcher(eval policy.PolicyEvaluator, exec tools.ToolExecutor, run policy.RunContext) *PolicyDispatcher {
	return &PolicyDispatcher{eval: eval, exec: exec, run: run}
}

// Call enforces policy then executes. A policy denial or executor error is returned as-is so
// the Server can surface it to the agent while Terfyn's trace records the denial.
func (d *PolicyDispatcher) Call(ctx context.Context, uses string, args map[string]any) (map[string]any, error) {
	if d.eval == nil || d.exec == nil {
		return nil, fmt.Errorf("mcpserver: dispatcher not fully configured")
	}
	stepID := fmt.Sprintf("mcp-%d", d.seq.Add(1))
	if err := d.eval.CheckToolCall(ctx, policy.ToolCallContext{
		Run:    d.run,
		StepID: stepID,
		Uses:   uses,
		With:   args,
	}); err != nil {
		return nil, err
	}
	resp, err := d.exec.Call(ctx, tools.ToolCallRequest{Uses: uses, With: args})
	if err != nil {
		return nil, err
	}
	return resp.Output, nil
}
