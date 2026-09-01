// Package claudecode is the external Claude Code agent-runtime target (epic #335): a reviewed
// .agent program driven by an external CLI agent (`claude -p`) that sees only the operations the
// grant compiled into a per-run Terfyn MCP server — every call still passes Terfyn policy /
// CheckToolCall / HITL, so the capability grant stays the boundary, not the harness prompt.
//
// This file is the #336 seam: the runtime name and adapter registration exist so
// `terfyn run --runtime claude-code` selects and dispatches here. The spawn / stream-json driver,
// the grant→MCP server (#338), the soundness guard (#339), budget mapping (#340), and trace
// integration (#341) land in their own issues; until then Invoke/Resume return a clear
// not-implemented error rather than silently doing nothing.
package claudecode

import (
	"context"
	"errors"

	"github.com/Terfyn/terfyn/internal/config"
	"github.com/Terfyn/terfyn/internal/runtime"
)

// Name is the --runtime selector and RuntimeTarget name for the external Claude Code runtime.
const Name = "claude-code"

// Runtime is the external-agent-runtime adapter. deps are the shared control-plane dependencies;
// the external driver is filled in by #337.
type Runtime struct {
	deps runtime.Deps
}

// NewFromDeps constructs the adapter. It is registered as the "claude-code" runtime factory.
func NewFromDeps(deps runtime.Deps) (runtime.Runtime, error) {
	return &Runtime{deps: deps}, nil
}

var errNotImplemented = errors.New(`runtime "claude-code": external agent-runtime adapter is not implemented yet (epic #335; adapter in #337)`)

// Invoke will spawn the external agent constrained by the grant-compiled MCP server (#337+). Stub.
func (r *Runtime) Invoke(_ context.Context, _ *config.ResolvedConfig, _ runtime.InvokeOptions) (runtime.RunResult, error) {
	return runtime.RunResult{}, errNotImplemented
}

// Resume will continue an external-runtime run from its checkpoint (#337+). Stub.
func (r *Runtime) Resume(_ context.Context, _ *config.ResolvedConfig, _ runtime.ResumeOptions) (runtime.RunResult, error) {
	return runtime.RunResult{}, errNotImplemented
}

// Health reports the adapter as degraded while it is a stub, so `terfyn run --runtime claude-code`
// selects it and fails loudly rather than the selector rejecting an unknown name.
func (r *Runtime) Health(_ context.Context) runtime.HealthStatus {
	return runtime.HealthStatus{State: runtime.HealthDegraded, Details: "claude-code external runtime is a stub (epic #335, adapter in #337)"}
}
