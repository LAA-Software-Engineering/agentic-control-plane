// Package claudecode is the external Claude Code agent-runtime target (epic #335): a reviewed
// .agent program driven by an external CLI agent (`claude -p`) that sees only the operations the
// grant compiled into a per-run Terfyn MCP server — every call still passes Terfyn policy /
// CheckToolCall / HITL, so the capability grant stays the boundary, not the harness prompt.
//
// The AgentRuntime adapter (spawn `claude -p`, stream-json parsing, error mapping) lands in #337 —
// see session.go / stream.go / claude.go. What remains before `terfyn run --runtime claude-code`
// executes a workflow is the integration: constructing the prompt from the .agent program and the
// per-run Terfyn MCP server that advertises exactly the granted operations (#338), budget mapping
// (#340), and trace/audit (#341). Until that lands, Invoke/Resume return a clear pending error
// rather than silently doing nothing; the adapter itself is real and unit-tested.
package claudecode

import (
	"context"
	"errors"

	"github.com/Terfyn/terfyn/internal/config"
	"github.com/Terfyn/terfyn/internal/runtime"
)

// Name is the --runtime selector and RuntimeTarget name for the external Claude Code runtime.
const Name = "claude-code"

// Runtime is the external-agent-runtime adapter wired into runtime.Runtime. agent is the boundary
// driver (#337); deps are the shared control-plane dependencies.
type Runtime struct {
	deps  runtime.Deps
	agent AgentRuntime
}

// NewFromDeps constructs the adapter with the default ClaudeCodeRuntime driver. Registered as the
// "claude-code" runtime factory.
func NewFromDeps(deps runtime.Deps) (runtime.Runtime, error) {
	return &Runtime{deps: deps, agent: ClaudeCodeRuntime{}}, nil
}

var errPendingIntegration = errors.New(`runtime "claude-code": the spawn/stream adapter is ready (#337); wiring a workflow run through it — prompt construction and the per-run Terfyn MCP tool server — lands in #338`)

// Invoke will drive a workflow through the external agent once the per-run MCP server (#338) exists.
func (r *Runtime) Invoke(_ context.Context, _ *config.ResolvedConfig, _ runtime.InvokeOptions) (runtime.RunResult, error) {
	return runtime.RunResult{}, errPendingIntegration
}

// Resume will continue an external-runtime run from its checkpoint (#338+).
func (r *Runtime) Resume(_ context.Context, _ *config.ResolvedConfig, _ runtime.ResumeOptions) (runtime.RunResult, error) {
	return runtime.RunResult{}, errPendingIntegration
}

// Health reports degraded until the run integration (#338) lands, so `terfyn run --runtime
// claude-code` selects the adapter and fails loudly rather than the selector rejecting the name.
func (r *Runtime) Health(_ context.Context) runtime.HealthStatus {
	return runtime.HealthStatus{State: runtime.HealthDegraded, Details: "claude-code adapter ready (#337); workflow-run integration pending (#338)"}
}
