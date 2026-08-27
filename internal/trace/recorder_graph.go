package trace

import (
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
)

// NewRecorderForGraph returns a recorder with redaction options from project spec.
func NewRecorderForGraph(rt state.RuntimeStore, g *spec.ProjectGraph) *Recorder {
	return &Recorder{
		RT:        rt,
		Redaction: NormalizeRedactionOptions(RedactionFromGraph(g)),
	}
}
