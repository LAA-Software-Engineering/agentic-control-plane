package telemetry

// gen_ai attribute keys aligned with WayFind for cross-tool trace interoperability.
const (
	AttrSystem                = "gen_ai.system"
	AttrOperationName         = "gen_ai.operation.name"
	AttrAgentName             = "gen_ai.agent.name"
	AttrAgentVersion          = "gen_ai.agent.version"
	AttrRunID                 = "gen_ai.run.id"
	AttrThreadID              = "gen_ai.thread.id"
	AttrRequestID             = "gen_ai.request.id"
	AttrActorID               = "gen_ai.actor.id"
	AttrTenantID              = "gen_ai.tenant.id"
	AttrResponseStatus        = "gen_ai.response.status"
	AttrRequestTraceID        = "gen_ai.request.trace_id"
	AttrWorkflow              = "gen_ai.workflow"
	AttrResponseHasToolCalls  = "gen_ai.response.has_tool_calls"
	AttrResponseToolCallCount = "gen_ai.response.tool_call_count"
	AttrToolTrusted           = "gen_ai.tool.trusted"
	AttrToolSideEffects       = "gen_ai.tool.side_effects"
	AttrToolRequiresApproval  = "gen_ai.tool.requires_approval"
	AttrHitlInterrupted       = "gen_ai.hitl.interrupted"
	AttrHitlResumed           = "gen_ai.hitl.resumed"
)

// Span names exported to OTLP backends.
const (
	SpanAgentRun  = "agent.run"
	SpanModelChat = "model.chat"
	SpanToolExec  = "tool.exec"
	SpanApproval  = "approval"
)

// Operation values for gen_ai.operation.name.
const (
	OpRun       = "run"
	OpModelChat = "model.chat"
	OpToolExec  = "tool.exec"
	OpApproval  = "approval"
)

// Response status values for gen_ai.response.status.
const (
	StatusOK    = "ok"
	StatusError = "error"
)

// SystemName identifies the emitter in gen_ai.system.
const SystemName = "agentctl"
