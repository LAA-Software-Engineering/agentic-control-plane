package telemetry

import (
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// Config is the runtime view of Project.spec.telemetry.
type Config struct {
	Enabled       bool
	ServiceName   string
	Endpoint      string
	ConsoleExport bool
}

// ConfigFromGraph returns telemetry settings from a merged project graph.
func ConfigFromGraph(g *spec.ProjectGraph) Config {
	if g == nil || g.Spec.Telemetry == nil {
		return Config{}
	}
	t := g.Spec.Telemetry
	return Config{
		Enabled:       t.Enabled,
		ServiceName:   strings.TrimSpace(t.ServiceName),
		Endpoint:      strings.TrimSpace(t.Endpoint),
		ConsoleExport: t.ConsoleExport,
	}
}
