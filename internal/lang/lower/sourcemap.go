package lower

import (
	"sort"

	"github.com/Terfyn/terfyn/internal/spec"
)

// SourceMap indexes lowered IR identities to their .agent authoring position.
//
// Positions are ALSO stamped on the IR nodes themselves (spec.Resource.Pos,
// WorkflowStep.*Pos, AgentSpec.ToolsPos — threaded through the pipeline by #187),
// and that is the primary way a downstream diagnostic underlines source. The
// SourceMap is the auxiliary index for the cases the node fields do not cover:
//
//   - a pass that holds a structural identity (a workflow/step id, an agent name)
//     but not the node, and wants a position without re-walking the graph;
//   - constructs with no dedicated Pos field on the resource model — the
//     unresolved type references and the workflow effects { } clause, which are
//     not lowered into resource-model fields (they belong to #193/#198/#190).
//
// Keys are structural, never source-derived, so they are stable across
// compilations. Use the Key* helpers to build them.
type SourceMap struct {
	pos map[string]spec.Pos
}

func newSourceMap() *SourceMap { return &SourceMap{pos: map[string]spec.Pos{}} }

func (m *SourceMap) set(key string, p spec.Pos) {
	if m == nil || key == "" {
		return
	}
	// First writer wins: the earliest (structurally outermost) position is the
	// most useful anchor, and re-setting would let ordering perturb the map.
	if _, ok := m.pos[key]; ok {
		return
	}
	m.pos[key] = p
}

// Lookup returns the recorded position for a structural key and whether one
// exists.
func (m *SourceMap) Lookup(key string) (spec.Pos, bool) {
	if m == nil {
		return spec.Pos{}, false
	}
	p, ok := m.pos[key]
	return p, ok
}

// Len reports how many entries the map holds.
func (m *SourceMap) Len() int {
	if m == nil {
		return 0
	}
	return len(m.pos)
}

// Keys returns the structural keys in sorted order (deterministic for tests and
// display).
func (m *SourceMap) Keys() []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m.pos))
	for k := range m.pos {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- Structural keys --------------------------------------------------------
//
// The forms are intentionally readable and collision-free across resource kinds.

// KeyAgent identifies an Agent resource: "Agent/<name>".
func KeyAgent(name string) string { return "Agent/" + name }

// KeyAgentGrant identifies one grant on an agent, by its reconstructed uses
// string: "Agent/<name>#grants/tool.github.read_pr".
func KeyAgentGrant(agent, uses string) string { return KeyAgent(agent) + "#grants/" + uses }

// KeyAgentType identifies an agent input/output type reference:
// "Agent/<name>#type/input".
func KeyAgentType(agent, which string) string { return KeyAgent(agent) + "#type/" + which }

// KeyAgentInstructions is the source-map key for an agent's instructions field.
func KeyAgentInstructions(agent string) string { return KeyAgent(agent) + "#instructions" }

// KeyWorkflow identifies a Workflow resource: "Workflow/<name>".
func KeyWorkflow(name string) string { return "Workflow/" + name }

// KeyStep identifies a lowered step: "Workflow/<wf>#steps/<id>".
func KeyStep(workflow, id string) string { return KeyWorkflow(workflow) + "#steps/" + id }

// KeyWorkflowType identifies a workflow param/result type reference:
// "Workflow/<wf>#type/<which>".
func KeyWorkflowType(workflow, which string) string {
	return KeyWorkflow(workflow) + "#type/" + which
}

// KeyWorkflowEffect identifies one entry of the effects { } clause:
// "Workflow/<wf>#effects/github.read".
func KeyWorkflowEffect(workflow, effect string) string {
	return KeyWorkflow(workflow) + "#effects/" + effect
}
