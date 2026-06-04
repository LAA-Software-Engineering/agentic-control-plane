package trace

import (
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// NewRecorderForGraph returns a recorder with redaction options from project spec.
func NewRecorderForGraph(rt state.RuntimeStore, g *spec.ProjectGraph) *Recorder {
	return &Recorder{
		RT:        rt,
		Redaction: NormalizeRedactionOptions(RedactionFromGraph(g)),
	}
}
