package trace

import "github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"

// Event is one persisted trace row (design doc §14.2); same shape as [state.TraceEvent].
type Event = state.TraceEvent

// Event type names from design doc §12.2 I (Trace recorder).
const (
	EventRunStarted      = "run.started"
	EventRunFinished     = "run.finished"
	EventStepStarted     = "step.started"
	EventStepFinished    = "step.finished"
	EventStepFailed      = "step.failed"
	EventToolCalled      = "tool.called"
	EventToolCompleted   = "tool.completed"
	EventModelCalled     = "model.called"
	EventModelCompleted  = "model.completed"
	EventPolicyDenied    = "policy.denied"
)
