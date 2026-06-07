package trace

import "github.com/LAA-Software-Engineering/agentic-control-plane/internal/telemetry"

// SpanName returns the OTel span name for et (issue #108).
func (e EventType) SpanName() string {
	switch e {
	case EventRunStarted, EventRunFinished, EventRunError:
		return telemetry.SpanAgentRun
	case EventLLMCompletion:
		return telemetry.SpanModelChat
	case EventToolSelection, EventToolExecution:
		return telemetry.SpanToolExec
	case EventHitlRequestCreated, EventHitlDecisionSubmitted, EventHitlResolutionApplied:
		return telemetry.SpanApproval
	case EventMemoryRead, EventMemoryWrite:
		return telemetry.SpanAgentRun
	case EventSystemError, EventLimitHit:
		return telemetry.SpanAgentRun
	default:
		return telemetry.SpanAgentRun
	}
}

// GenAIOperation returns gen_ai.operation.name for et (issue #108).
func (e EventType) GenAIOperation() string {
	switch e {
	case EventRunStarted, EventRunFinished, EventRunError:
		return telemetry.OpRun
	case EventLLMCompletion:
		return telemetry.OpModelChat
	case EventToolSelection, EventToolExecution:
		return telemetry.OpToolExec
	case EventHitlRequestCreated, EventHitlDecisionSubmitted, EventHitlResolutionApplied:
		return telemetry.OpApproval
	default:
		return telemetry.OpRun
	}
}

// TimelineIcon returns a short glyph for inspector timeline rendering (issue #109).
func (e EventType) TimelineIcon() string {
	switch e {
	case EventRunStarted:
		return "▶"
	case EventRunFinished:
		return "✓"
	case EventRunError:
		return "✗"
	case EventLLMCompletion:
		return "◆"
	case EventToolSelection:
		return "◎"
	case EventToolExecution:
		return "⚙"
	case EventHitlRequestCreated:
		return "?"
	case EventHitlDecisionSubmitted:
		return "☑"
	case EventHitlResolutionApplied:
		return "→"
	case EventMemoryRead:
		return "↙"
	case EventMemoryWrite:
		return "↗"
	case EventSystemError, EventLimitHit:
		return "!"
	default:
		return "·"
	}
}

// TimelineGroup returns a coarse grouping label for inspector filters.
func (e EventType) TimelineGroup() string {
	switch e {
	case EventRunStarted, EventRunFinished, EventRunError:
		return "run"
	case EventLLMCompletion:
		return "llm"
	case EventToolSelection, EventToolExecution:
		return "tool"
	case EventHitlRequestCreated, EventHitlDecisionSubmitted, EventHitlResolutionApplied:
		return "hitl"
	case EventMemoryRead, EventMemoryWrite:
		return "memory"
	case EventSystemError, EventLimitHit:
		return "system"
	default:
		return "other"
	}
}
