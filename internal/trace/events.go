package trace

import "github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"

// Event is one persisted trace row (design doc §14.2); same shape as [state.TraceEvent].
type Event = state.TraceEvent
