package lang

// Inline resource declarations (ADR 005, issue #333): top-level `tool { … }` and `policy { … }`
// blocks in .agent that lower to the same spec.ToolResource / spec.PolicyResource the YAML loader
// produces. Field pointers are nil when omitted; presence of a block is significant where the IR is
// presence-sensitive (notably ToolDecl.Operations — see the lowering, which sets OperationsDeclared).

// ToolDecl is a `tool <Name> { … }` declaration.
type ToolDecl struct {
	Pos    Pos
	Name   *Ident
	Type   *Ident           // type <native|mock|mcp|http>
	MCP    *ToolMCPBlock    // mcp { … } transport config (issue #440)
	HTTP   *ToolHTTPBlock   // http { … } transport config (issue #440)
	Safety *ToolSafetyBlock // safety { … }
	// Operations is nil when the block is omitted (open callable set) and non-nil when present,
	// including an empty `operations {}` (a closed, deny-all manifest). This distinction is the
	// OperationsDeclared bit (#204) and MUST survive lowering.
	Operations *ToolOperations
}

// ToolMCPBlock is `mcp { transport … command … args { … } url … headers { … } }` (issue #440).
type ToolMCPBlock struct {
	Pos       Pos
	Transport *StringLit
	Command   *StringLit
	Args      []*StringLit
	URL       *StringLit
	Headers   []*HeaderPair
}

// ToolHTTPBlock is `http { baseUrl … headers { … } }` (issue #440).
type ToolHTTPBlock struct {
	Pos     Pos
	BaseURL *StringLit
	Headers []*HeaderPair
}

// HeaderPair is one `"<key>" "<value>"` entry in a headers { … } block.
type HeaderPair struct {
	Pos   Pos
	Key   *StringLit
	Value *StringLit
}

func (d *ToolDecl) Position() Pos { return d.Pos }
func (d *ToolDecl) declNode()     {}

// ToolSafetyBlock is `safety { trusted … sideEffects … requiresApproval … }`; each field is nil when omitted.
type ToolSafetyBlock struct {
	Pos              Pos
	Trusted          *bool
	SideEffects      *bool
	RequiresApproval *bool
}

// ToolOperations is the `operations { … }` block. Ops may be empty (an explicit `operations {}`).
type ToolOperations struct {
	Pos Pos
	Ops []*ToolOperationDecl
}

// ToolOperationDecl is one `"<op>" { effects { … } }` entry in the operations block.
type ToolOperationDecl struct {
	Pos     Pos
	Name    *Ident
	Effects []*EffectRef // effects { … } (bare dotted idents); nil when omitted
}

// PolicyDecl is a `policy <Name> { … }` declaration.
type PolicyDecl struct {
	Pos       Pos
	Name      *Ident
	Preset    *Ident // preset <name>; a built-in policy preset (e.g. shell_safe). nil if omitted.
	Execution *PolicyExecutionBlock
	Approvals *PolicyApprovalsBlock
	Effects   *PolicyEffectsBlock
}

func (d *PolicyDecl) Position() Pos { return d.Pos }
func (d *PolicyDecl) declNode()     {}

// PolicyExecutionBlock is `execution { maxTotalCostUsd … maxWallClockSeconds … requireStructuredOutput … }`.
type PolicyExecutionBlock struct {
	Pos                     Pos
	MaxTotalCostUsd         *float64
	MaxWallClockSeconds     *int
	RequireStructuredOutput *bool
}

// PolicyApprovalsBlock is `approvals { requiredFor { … } requireAllTools … permissive … }`.
type PolicyApprovalsBlock struct {
	Pos             Pos
	RequiredFor     []*Grant // tool.<name>.<operation> uses strings
	RequireAllTools *bool
	Permissive      *bool
}

// PolicyEffectsBlock is `effects { permit { … } permitWithApproval { … } }`. Both are first-class:
// permitted autonomously versus permitted only behind approval (ADR 005 §4).
type PolicyEffectsBlock struct {
	Pos                Pos
	Permit             []*EffectRef
	PermitWithApproval []*EffectRef
}
