package local

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// Compile-time check that [Runtime] implements the runtime adapter contract.
var _ runtime.Runtime = (*Runtime)(nil)

// Runtime is the MVP local workflow runner backend (issue #23): SQLite state + resolved config snapshot.
type Runtime struct {
	Store        state.RuntimeStore
	Now          func() time.Time
	AgentVersion string
}

// NewFromDeps constructs a local runtime from control-plane dependencies.
func NewFromDeps(deps runtime.Deps) (runtime.Runtime, error) {
	if deps.Store == nil {
		return nil, fmt.Errorf("local: nil runtime store")
	}
	return &Runtime{
		Store:        deps.Store,
		Now:          deps.Now,
		AgentVersion: deps.AgentVersion,
	}, nil
}

// NewRuntime returns a local runtime backed by store. Prefer [NewFromDeps] via [runtime.Lookup].
func NewRuntime(store state.RuntimeStore) *Runtime {
	rt, err := NewFromDeps(runtime.Deps{Store: store})
	if err != nil {
		panic(err)
	}
	return rt.(*Runtime)
}

func (r *Runtime) now() time.Time {
	if r != nil && r.Now != nil {
		return r.Now()
	}
	return time.Now().UTC()
}

func (r *Runtime) agentVersion() string {
	if r == nil {
		return "unknown"
	}
	if v := strings.TrimSpace(r.AgentVersion); v != "" {
		return v
	}
	return "0.0.0-dev"
}

// Health reports local runtime readiness based on store availability.
func (r *Runtime) Health(_ context.Context) runtime.HealthStatus {
	if r == nil || r.Store == nil {
		return runtime.HealthStatus{
			State:   runtime.HealthError,
			Details: "nil runtime store",
		}
	}
	return runtime.HealthStatus{State: runtime.HealthOK}
}
