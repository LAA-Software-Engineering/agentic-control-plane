package plan

import (
	"context"
	"errors"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// Action* are [Operation.Action] values (design doc §12.2).
const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
)

// Plan is the result of comparing desired project resources to stored deployment rows (§12.2).
type Plan struct {
	Operations []Operation
	Risk       RiskSummary
	// DeploymentBaseline is a digest of applied_resources + applied_projects for this env at plan time.
	// Apply rejects the plan if the store diverges (exit code 3; issue #78).
	DeploymentBaseline string
}

// Operation is one create, update, or delete against a resource identity.
// SpecHash and NormalizedSpecJSON are set for create and update (apply material); they are empty for delete.
type Operation struct {
	Action             string
	Target             spec.ResourceID
	Diff               []FieldChange
	SpecHash           string
	NormalizedSpecJSON string
}

// FieldChange is one normalized field-level delta for updates (§10.2 plan output).
type FieldChange struct {
	Path string
	Old  string
	New  string
}

// RiskCategory classifies a plan risk item (issue #165). Values are stable for JSON/YAML CI gates.
type RiskCategory string

const (
	RiskCategoryPermissionWidening   RiskCategory = "permission_widening"
	RiskCategoryApprovalRemoval      RiskCategory = "approval_removal"
	RiskCategoryBudgetRelaxation     RiskCategory = "budget_relaxation"
	RiskCategoryModelChange          RiskCategory = "model_change"
	RiskCategoryToolSurfaceChange    RiskCategory = "tool_surface_change"
	RiskCategorySafety               RiskCategory = "safety"
	RiskCategoryLint                 RiskCategory = "lint"
	RiskCategoryEffectPermitWidening RiskCategory = "effect_permit_widening"
)

// RiskSeverity is high / medium / low. Approval removal, write-like permission widening,
// and budget relaxation are high; model change is medium; tool surface change is medium
// unless the added tool is write-like (then high).
type RiskSeverity string

const (
	RiskSeverityHigh   RiskSeverity = "high"
	RiskSeverityMedium RiskSeverity = "medium"
	RiskSeverityLow    RiskSeverity = "low"
)

// RiskTargetKind is the resource kind a risk item points at.
type RiskTargetKind string

const (
	RiskTargetPolicy RiskTargetKind = "policy"
	RiskTargetAgent  RiskTargetKind = "agent"
	RiskTargetTool   RiskTargetKind = "tool"
)

// RiskTarget identifies the changed resource for a [RiskItem].
type RiskTarget struct {
	Kind RiskTargetKind `json:"kind" yaml:"kind"`
	Name string         `json:"name" yaml:"name"`
}

// WitnessHopKind is one node on a structured witness path (ADR 002 / issue #165).
// C1 uses resource-level hops; #191 may add workflow → step → agent → tool.operation.
type WitnessHopKind string

const (
	WitnessKindWorkflow      WitnessHopKind = "workflow"
	WitnessKindStep          WitnessHopKind = "step"
	WitnessKindAgent         WitnessHopKind = "agent"
	WitnessKindToolOperation WitnessHopKind = "tool_operation"
	WitnessKindPolicy        WitnessHopKind = "policy"
	WitnessKindTool          WitnessHopKind = "tool"
)

// WitnessReachability tags whether a hop is a declared graph edge or an autonomous choice.
type WitnessReachability string

const (
	WitnessStatic     WitnessReachability = "static"
	WitnessAutonomous WitnessReachability = "autonomous"
)

// WitnessHop is one edge on a structured path from a workflow (or other root) to a
// concrete tool operation. C1 does not compute effect bounds; hops are typically a
// single static resource. #191 can append hops without a parallel rendering path.
type WitnessHop struct {
	Kind         WitnessHopKind      `json:"kind" yaml:"kind"`
	Name         string              `json:"name,omitempty" yaml:"name,omitempty"`
	ID           string              `json:"id,omitempty" yaml:"id,omitempty"`
	Reachability WitnessReachability `json:"reachability" yaml:"reachability"`
}

// RiskItem is one labeled plan-risk finding (issue #165).
type RiskItem struct {
	Category RiskCategory `json:"category" yaml:"category"`
	Severity RiskSeverity `json:"severity" yaml:"severity"`
	Reason   string       `json:"reason" yaml:"reason"`
	Target   RiskTarget   `json:"target" yaml:"target"`
	Witness  []WitnessHop `json:"witness,omitempty" yaml:"witness,omitempty"`
}

// RiskSummary carries MVP plan risk signals (design doc §12.2, §10.2, issue #165).
// Messages is the reason text of Items (stable for existing string consumers).
type RiskSummary struct {
	Messages []string
	Items    []RiskItem
	// Lint holds structured policy lint findings (issue #107).
	Lint []policy.LintFinding
}

// Planner reads deployment state to compare desired vs applied resources (design doc §5.2).
type Planner struct {
	Deploy state.DeploymentStore
}

// NewPlanner returns a planner backed by dep. dep must not be nil when methods are called.
func NewPlanner(dep state.DeploymentStore) *Planner {
	return &Planner{Deploy: dep}
}

// ListAppliedResources returns applied resources for env (MVP entry point for plan input).
func (p *Planner) ListAppliedResources(ctx context.Context, env string) ([]state.AppliedResource, error) {
	if p == nil || p.Deploy == nil {
		return nil, errors.New("plan: nil deployment store")
	}
	return p.Deploy.ListAppliedResourcesByEnv(ctx, env)
}
