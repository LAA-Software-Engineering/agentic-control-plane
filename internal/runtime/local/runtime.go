package local

import (
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// Runtime is the MVP local workflow runner backend (issue #23): project root on disk + SQLite (or any [state.RuntimeStore]).
type Runtime struct {
	ProjectRoot  string
	Store        state.RuntimeStore
	Now          func() time.Time
	AgentVersion string
}

// NewRuntime returns a local runtime. projectRoot is the directory containing project.yaml.
func NewRuntime(projectRoot string, store state.RuntimeStore) *Runtime {
	return &Runtime{ProjectRoot: projectRoot, Store: store}
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

// ApplyEnvironment delegates to [spec.ApplyEnvironment] for backward compatibility.
func ApplyEnvironment(g *spec.ProjectGraph, envName string) (*spec.ProjectGraph, error) {
	return spec.ApplyEnvironment(g, envName)
}
