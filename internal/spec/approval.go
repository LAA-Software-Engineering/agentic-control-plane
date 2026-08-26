package spec

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ApprovalStepUses is the sentinel uses string stored on workflow-level HITL
// checkpoints (issue #195). It is not a tool identity; switch is not applicable.
const ApprovalStepUses = "workflow.approval"

// DefaultApprovalStepDescription is shown when an approval step omits description.
const DefaultApprovalStepDescription = "Workflow step requires approval"

// WorkflowApprovalConfig is optional review presentation on an approval step.
// These fields are not policy: they do not decide whether the step pauses.
type WorkflowApprovalConfig struct {
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	RedactKeys  []string `yaml:"redactKeys,omitempty" json:"redactKeys,omitempty"`
}

// WorkflowApprovalValue is either enabled-with-defaults (true) or an explicit [WorkflowApprovalConfig].
type WorkflowApprovalValue struct {
	Enabled bool
	Config  *WorkflowApprovalConfig
}

// StepIsApproval reports whether st is an approval graph node (issue #195).
func StepIsApproval(st WorkflowStep) bool {
	return st.Approval != nil && st.Approval.Enabled
}

// UnmarshalYAML accepts `true` or a mapping for approval:.
func (v *WorkflowApprovalValue) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	switch value.Kind {
	case yaml.ScalarNode:
		var b bool
		if err := value.Decode(&b); err != nil {
			return fmt.Errorf("spec: approval must be true or a config object: %w", err)
		}
		if !b {
			return fmt.Errorf("spec: approval must be true or a config object, not false")
		}
		v.Enabled = true
		return nil
	case yaml.MappingNode:
		var cfg WorkflowApprovalConfig
		if err := decodeYAMLNodeKnownFields(value, &cfg); err != nil {
			return err
		}
		v.Enabled = true
		v.Config = &cfg
		return nil
	default:
		return fmt.Errorf("spec: approval must be true or a config object")
	}
}

// MarshalYAML encodes as true or the config object.
func (v WorkflowApprovalValue) MarshalYAML() (any, error) {
	if v.Config != nil {
		return v.Config, nil
	}
	if v.Enabled {
		return true, nil
	}
	return nil, nil
}

// ApprovalStepDescription returns review text for an approval step.
func ApprovalStepDescription(st WorkflowStep) string {
	if st.Approval != nil && st.Approval.Config != nil {
		if d := strings.TrimSpace(st.Approval.Config.Description); d != "" {
			return d
		}
	}
	return DefaultApprovalStepDescription
}
