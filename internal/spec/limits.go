package spec

import (
	"fmt"
	"strings"
)

// Default byte limits for execution bounds (issue #117). Values use binary KiB/MiB.
const (
	DefaultMaxToolInputBytes  = 256 << 10 // 256 KiB
	DefaultMaxToolOutputBytes = 256 << 10 // 256 KiB
	DefaultMaxCheckpointBytes = 1 << 20   // 1 MiB
)

// LimitExceedPolicy controls behavior when a byte limit is exceeded.
type LimitExceedPolicy string

const (
	// LimitExceedTruncate shortens payload fields in-place (engine truncateMapInPlace) and records a limit-hit trace event.
	LimitExceedTruncate LimitExceedPolicy = "truncate"
	// LimitExceedFail aborts the step or run with a clear error.
	LimitExceedFail LimitExceedPolicy = "fail"
)

// LimitKind identifies which execution limit was evaluated (trace events, issue #117).
type LimitKind string

const (
	LimitKindToolInput  LimitKind = "tool_input"
	LimitKindToolOutput LimitKind = "tool_output"
	LimitKindCheckpoint LimitKind = "checkpoint"
)

// ExecutionLimits bounds tool I/O and checkpoint/state size. Project YAML may set defaults;
// Workflow and Tool resources may override individual fields (issue #117).
type ExecutionLimits struct {
	MaxToolInputBytes      int               `yaml:"maxToolInputBytes,omitempty" json:"maxToolInputBytes,omitempty"`
	MaxToolOutputBytes     int               `yaml:"maxToolOutputBytes,omitempty" json:"maxToolOutputBytes,omitempty"`
	MaxCheckpointBytes     int               `yaml:"maxCheckpointBytes,omitempty" json:"maxCheckpointBytes,omitempty"`
	MaxStateBytes          int               `yaml:"maxStateBytes,omitempty" json:"maxStateBytes,omitempty"`
	ToolInputExceedPolicy  LimitExceedPolicy `yaml:"toolInputExceedPolicy,omitempty" json:"toolInputExceedPolicy,omitempty"`
	ToolOutputExceedPolicy LimitExceedPolicy `yaml:"toolOutputExceedPolicy,omitempty" json:"toolOutputExceedPolicy,omitempty"`
	CheckpointExceedPolicy LimitExceedPolicy `yaml:"checkpointExceedPolicy,omitempty" json:"checkpointExceedPolicy,omitempty"`
}

// ResolvedExecutionLimits holds fully merged limits after precedence resolution.
type ResolvedExecutionLimits struct {
	MaxToolInputBytes      int
	MaxToolOutputBytes     int
	MaxCheckpointBytes     int
	ToolInputExceedPolicy  LimitExceedPolicy
	ToolOutputExceedPolicy LimitExceedPolicy
	CheckpointExceedPolicy LimitExceedPolicy
}

// DefaultExecutionLimits returns built-in limits when project config omits a limits block.
func DefaultExecutionLimits() ResolvedExecutionLimits {
	return ResolvedExecutionLimits{
		MaxToolInputBytes:      DefaultMaxToolInputBytes,
		MaxToolOutputBytes:     DefaultMaxToolOutputBytes,
		MaxCheckpointBytes:     DefaultMaxCheckpointBytes,
		ToolInputExceedPolicy:  LimitExceedTruncate,
		ToolOutputExceedPolicy: LimitExceedTruncate,
		CheckpointExceedPolicy: LimitExceedFail,
	}
}

// NormalizeExecutionLimits merges maxStateBytes into maxCheckpointBytes when the latter is unset.
func NormalizeExecutionLimits(l *ExecutionLimits) {
	if l == nil {
		return
	}
	if l.MaxCheckpointBytes <= 0 && l.MaxStateBytes > 0 {
		l.MaxCheckpointBytes = l.MaxStateBytes
	}
}

// MergeExecutionLimits overlays override onto base; non-zero override fields win.
func MergeExecutionLimits(base ExecutionLimits, override *ExecutionLimits) ExecutionLimits {
	if override == nil {
		return base
	}
	NormalizeExecutionLimits(override)
	out := base
	if override.MaxToolInputBytes > 0 {
		out.MaxToolInputBytes = override.MaxToolInputBytes
	}
	if override.MaxToolOutputBytes > 0 {
		out.MaxToolOutputBytes = override.MaxToolOutputBytes
	}
	if override.MaxCheckpointBytes > 0 {
		out.MaxCheckpointBytes = override.MaxCheckpointBytes
	}
	if p := strings.TrimSpace(string(override.ToolInputExceedPolicy)); p != "" {
		out.ToolInputExceedPolicy = LimitExceedPolicy(p)
	}
	if p := strings.TrimSpace(string(override.ToolOutputExceedPolicy)); p != "" {
		out.ToolOutputExceedPolicy = LimitExceedPolicy(p)
	}
	if p := strings.TrimSpace(string(override.CheckpointExceedPolicy)); p != "" {
		out.CheckpointExceedPolicy = LimitExceedPolicy(p)
	}
	NormalizeExecutionLimits(&out)
	return out
}

// ResolveExecutionLimits merges project, workflow, and tool limits with built-in defaults.
// Precedence (highest wins): tool > workflow > project > defaults.
func ResolveExecutionLimits(project *ProjectSpec, workflow *WorkflowSpec, tool *ToolSpec) ResolvedExecutionLimits {
	def := DefaultExecutionLimits()
	merged := ExecutionLimits{
		MaxToolInputBytes:      def.MaxToolInputBytes,
		MaxToolOutputBytes:     def.MaxToolOutputBytes,
		MaxCheckpointBytes:     def.MaxCheckpointBytes,
		ToolInputExceedPolicy:  def.ToolInputExceedPolicy,
		ToolOutputExceedPolicy: def.ToolOutputExceedPolicy,
		CheckpointExceedPolicy: def.CheckpointExceedPolicy,
	}
	if project != nil && project.Limits != nil {
		merged = MergeExecutionLimits(merged, project.Limits)
	}
	if workflow != nil && workflow.Limits != nil {
		merged = MergeExecutionLimits(merged, workflow.Limits)
	}
	if tool != nil && tool.Limits != nil {
		merged = MergeExecutionLimits(merged, tool.Limits)
	}
	return ResolvedExecutionLimits{
		MaxToolInputBytes:      merged.MaxToolInputBytes,
		MaxToolOutputBytes:     merged.MaxToolOutputBytes,
		MaxCheckpointBytes:     merged.MaxCheckpointBytes,
		ToolInputExceedPolicy:  merged.ToolInputExceedPolicy,
		ToolOutputExceedPolicy: merged.ToolOutputExceedPolicy,
		CheckpointExceedPolicy: merged.CheckpointExceedPolicy,
	}
}

// ValidateExecutionLimits returns an error when limits or policies are invalid.
func ValidateExecutionLimits(l *ExecutionLimits) error {
	if l == nil {
		return nil
	}
	NormalizeExecutionLimits(l)
	if err := validateLimitBytes("maxToolInputBytes", l.MaxToolInputBytes); err != nil {
		return err
	}
	if err := validateLimitBytes("maxToolOutputBytes", l.MaxToolOutputBytes); err != nil {
		return err
	}
	if err := validateLimitBytes("maxCheckpointBytes", l.MaxCheckpointBytes); err != nil {
		return err
	}
	if err := validateLimitBytes("maxStateBytes", l.MaxStateBytes); err != nil {
		return err
	}
	if err := validateExceedPolicy("toolInputExceedPolicy", l.ToolInputExceedPolicy); err != nil {
		return err
	}
	if err := validateExceedPolicy("toolOutputExceedPolicy", l.ToolOutputExceedPolicy); err != nil {
		return err
	}
	if err := validateExceedPolicy("checkpointExceedPolicy", l.CheckpointExceedPolicy); err != nil {
		return err
	}
	if l.CheckpointExceedPolicy == LimitExceedTruncate {
		return fmt.Errorf("spec: checkpointExceedPolicy %q is unsafe; use %q", LimitExceedTruncate, LimitExceedFail)
	}
	return nil
}

func validateLimitBytes(field string, n int) error {
	if n < 0 {
		return fmt.Errorf("spec: %s must be non-negative, got %d", field, n)
	}
	return nil
}

func validateExceedPolicy(field string, p LimitExceedPolicy) error {
	switch LimitExceedPolicy(strings.TrimSpace(string(p))) {
	case "", LimitExceedTruncate, LimitExceedFail:
		return nil
	default:
		return fmt.Errorf("spec: %s must be %q or %q, got %q", field, LimitExceedTruncate, LimitExceedFail, p)
	}
}

// validateExecutionLimitsGraph validates limits blocks on project, workflows, and tools.
func validateExecutionLimitsGraph(g *ProjectGraph) []error {
	if g == nil {
		return nil
	}
	var errs []error
	if err := ValidateExecutionLimits(g.Spec.Limits); err != nil {
		errs = append(errs, fmt.Errorf("Project: spec.limits: %w", err))
	}
	for name, wr := range g.Workflows {
		if wr == nil || wr.Spec.Limits == nil {
			continue
		}
		if err := ValidateExecutionLimits(wr.Spec.Limits); err != nil {
			errs = append(errs, fmt.Errorf("Workflow/%s: spec.limits: %w", name, err))
		}
	}
	for name, tr := range g.Tools {
		if tr == nil || tr.Spec.Limits == nil {
			continue
		}
		if err := ValidateExecutionLimits(tr.Spec.Limits); err != nil {
			errs = append(errs, fmt.Errorf("Tool/%s: spec.limits: %w", name, err))
		}
	}
	return errs
}
