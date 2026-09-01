package trace

import (
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/state"
)

// NewRecorderForGraph returns a recorder with redaction options from project spec.
func NewRecorderForGraph(rt state.RuntimeStore, g *spec.ProjectGraph) *Recorder {
	return &Recorder{
		RT:        rt,
		Redaction: NormalizeRedactionOptions(RedactionFromGraph(g)),
	}
}
