package agentcli

import (
	"context"
	"fmt"

	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/runtime/mcpserver"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/tools"
	"github.com/Terfyn/terfyn/internal/trace"
)

// ExternalAgentRun is one external-agent invocation composed from a resolved run: the agent to
// drive, the graph its grants compile against, the policy evaluator and tool executor that enforce
// each call, and the run-level context (approvals, accumulated cost). It is assembled by the
// runtime's Invoke path (issue #367); the fields it needs are explicit so the driver stays testable
// with a fake process and no control-plane coupling.
type ExternalAgentRun struct {
	Graph     *spec.ProjectGraph
	Agent     *spec.AgentResource
	Eval      policy.PolicyEvaluator
	Exec      tools.ToolExecutor
	Recorder  *trace.Recorder // nil disables trace emission
	RunID     string
	Prompt    string            // the workflow input, rendered as the agent's user turn
	Run       policy.RunContext // approvals / startedAt / accumulated cost
	Limits    Limits
	ConfigDir string // directory to write the per-run --mcp-config into
	ExtraArgs []string
}

// RunExternalAgent drives one agent through the given external CLI driver, constrained to its
// grants. It is the CLI-agnostic composition shared by every runtime target: the driver
// (AgentRuntime) is the only per-CLI seam — everything around it is identical whichever CLI runs
// the program.
//
//   - compile the agent's grants into a per-run MCP server (closed-world tools/list, #338),
//   - serve it over an authenticated loopback endpoint bound to a per-run bearer token (#367),
//   - spawn the external agent (driver.RunSession) constrained to the per-run MCP server so those
//     grants are the ONLY tools it sees, and every tools/call routes through policy CheckToolCall ->
//     Tools.Call,
//   - fold the agent's turns into the run's hash-linked trace (#341), and
//   - keep Terfyn the enforcer of record: fold the session cost and re-check the budget (#340), so a
//     breach fails closed with limit_hit rather than trusting the harness's own accounting.
//
// It returns the parsed Session, the run context with the session cost folded in, and an error: a
// spawn/stream failure, a session that ended in error or max-turns, or a budget breach. The per-run
// MCP server is torn down before it returns.
func RunExternalAgent(ctx context.Context, driver AgentRuntime, in ExternalAgentRun) (Session, policy.RunContext, error) {
	if in.Agent == nil {
		return Session{}, in.Run, fmt.Errorf("agentcli: nil agent")
	}
	agentName := in.Agent.Metadata.Name

	compiled, err := mcpserver.Compile(in.Graph, agentName)
	if err != nil {
		return Session{}, in.Run, err
	}
	disp := mcpserver.NewPolicyDispatcher(in.Eval, in.Exec, in.Run).WithTrace(in.Recorder, in.RunID)
	srv := mcpserver.NewServer(compiled, disp, "terfyn")

	transport, stop, err := srv.ListenLocal()
	if err != nil {
		return Session{}, in.Run, fmt.Errorf("agentcli: start per-run MCP server: %w", err)
	}
	defer stop()

	cfgPath, err := mcpserver.WriteMCPConfig(in.ConfigDir, "terfyn", transport)
	if err != nil {
		return Session{}, in.Run, err
	}

	rs := RunSpec{
		Prompt:       in.Prompt,
		SystemPrompt: in.Agent.Spec.Instructions,
		MCPConfig:    cfgPath,
		ExtraArgs:    in.ExtraArgs,
	}
	in.Limits.ApplyTo(&rs)

	ctx2, cancel := in.Limits.WithTimeout(ctx)
	defer cancel()

	session, runErr := driver.RunSession(ctx2, rs)

	// Fold the agent's turns into the audit chain. Best-effort: a trace-store error never masks the
	// run outcome (the tool-call events were already emitted by the dispatcher during the run).
	if in.Recorder != nil {
		_ = EmitSessionTurns(ctx, in.Recorder, in.RunID, agentName, session)
	}
	if runErr != nil {
		return session, in.Run, runErr
	}

	run, budgetErr := EnforceBudget(ctx, in.Eval, in.Run, session)
	if budgetErr != nil {
		if in.Recorder != nil {
			_ = EmitLimitHit(ctx, in.Recorder, in.RunID, "budget", budgetErr)
		}
		return session, run, budgetErr
	}
	return session, run, nil
}
