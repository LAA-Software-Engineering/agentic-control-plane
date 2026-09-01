package trace

import "github.com/Terfyn/terfyn/internal/state"

// Event is one persisted trace row (design doc §14.2); same shape as [state.TraceEvent].
type Event = state.TraceEvent
