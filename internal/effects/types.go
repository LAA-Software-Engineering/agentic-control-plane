package effects

// Reachability tags whether a hop is a declared graph edge or an autonomous choice.
// Values match plan.WitnessReachability so issue #191 can map hops without importing plan.
type Reachability string

const (
	Static     Reachability = "static"
	Autonomous Reachability = "autonomous"
)

// HopKind is one node on a witness path. Values match plan.WitnessHopKind.
type HopKind string

const (
	KindWorkflow      HopKind = "workflow"
	KindStep          HopKind = "step"
	KindAgent         HopKind = "agent"
	KindToolOperation HopKind = "tool_operation"
)

// Hop is one edge on a structured path from a workflow (or agent) root to a
// concrete tool operation. Fields are compatible with plan.WitnessHop (issue #191).
// Pos is not included — it is metadata, not identity.
type Hop struct {
	Kind         HopKind      `json:"kind" yaml:"kind"`
	Name         string       `json:"name,omitempty" yaml:"name,omitempty"`
	ID           string       `json:"id,omitempty" yaml:"id,omitempty"`
	Reachability Reachability `json:"reachability" yaml:"reachability"`
}

// Effect is one reachable named effect (or an explicit unknown sentinel) plus a witness.
type Effect struct {
	// Ident is the declared effect identifier. Empty when Unknown is true.
	Ident string `json:"ident,omitempty" yaml:"ident,omitempty"`
	// Unknown is true when a reachable operation has no declared effects
	// ([spec.ResolveOperationEffects]). Not an empty/allow set.
	Unknown bool `json:"unknown,omitempty" yaml:"unknown,omitempty"`
	// Message names the tool/operation when Unknown is true.
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
	// Uses is the witnessing tool.<name>.<operation>.
	Uses string `json:"uses,omitempty" yaml:"uses,omitempty"`
	// Witness is at least one path Workflow → step → Agent → tool.operation
	// (agent-only roots omit workflow/step hops). If any witnessing path is
	// autonomous, Witness is that path (path-max), not the first static uses:.
	Witness []Hop `json:"witness,omitempty" yaml:"witness,omitempty"`
	// occurrences is every reachable op that declares Ident. [Check] uses this
	// so requiresApproval is not limited to the first-witness Uses.
	occurrences []effectOccurrence
}

// effectOccurrence is one reachable tool operation that contributes Ident.
type effectOccurrence struct {
	uses    string
	witness []Hop
}

// Unreachable is a declared effect (or fail-closed unknown) on a tool operation
// that is not reachable from the bound root. Reported, not omitted.
type Unreachable struct {
	Ident     string `json:"ident,omitempty" yaml:"ident,omitempty"`
	Unknown   bool   `json:"unknown,omitempty" yaml:"unknown,omitempty"`
	Tool      string `json:"tool,omitempty" yaml:"tool,omitempty"`
	Operation string `json:"operation,omitempty" yaml:"operation,omitempty"`
	Uses      string `json:"uses,omitempty" yaml:"uses,omitempty"`
}

// Bound is the transitive effect set of one agent or workflow.
type Bound struct {
	RootKind    HopKind       `json:"rootKind" yaml:"rootKind"`
	RootName    string        `json:"rootName" yaml:"rootName"`
	Effects     []Effect      `json:"effects,omitempty" yaml:"effects,omitempty"`
	Unreachable []Unreachable `json:"unreachable,omitempty" yaml:"unreachable,omitempty"`
}

// GraphBounds holds a bound for every agent and workflow in the graph.
type GraphBounds struct {
	Agents    map[string]Bound `json:"agents,omitempty" yaml:"agents,omitempty"`
	Workflows map[string]Bound `json:"workflows,omitempty" yaml:"workflows,omitempty"`
}
