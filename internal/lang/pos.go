package lang

import "github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"

// Pos is the source position carried by every token and AST node. It is a type
// alias for spec.Pos so .agent positions are the *same* type as the IR
// positions threaded through the resource model by #187 — a lowering pass (#197)
// can copy an AST node's Pos onto an IR node with no conversion.
type Pos = spec.Pos
