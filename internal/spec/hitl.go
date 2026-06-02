package spec

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// HitlDecisionKind names an operator resolution at an approval gate (issue #106).
type HitlDecisionKind string

const (
	HitlDecisionApprove HitlDecisionKind = "approve"
	HitlDecisionReject  HitlDecisionKind = "reject"
	HitlDecisionEdit    HitlDecisionKind = "edit"
	HitlDecisionSwitch  HitlDecisionKind = "switch"
)

// AllHitlDecisionKinds lists every supported decision in stable order.
var AllHitlDecisionKinds = []HitlDecisionKind{
	HitlDecisionApprove,
	HitlDecisionReject,
	HitlDecisionEdit,
	HitlDecisionSwitch,
}

// DefaultHitlDescriptionPrefix is shown before per-call review text when policy omits descriptionPrefix.
const DefaultHitlDescriptionPrefix = "Tool execution requires approval"

// HitlPolicy configures human-in-the-loop approval gates (issue #106).
type HitlPolicy struct {
	// InterruptOn maps tool metadata.name to true (defaults) or a per-tool review config.
	InterruptOn map[string]HitlInterruptValue `yaml:"interruptOn,omitempty" json:"interruptOn,omitempty"`
	// DescriptionPrefix prefixes every approval prompt (default [DefaultHitlDescriptionPrefix]).
	DescriptionPrefix string `yaml:"descriptionPrefix,omitempty" json:"descriptionPrefix,omitempty"`
	// ToolSwitchMap maps source operation to allowed target operations for switch decisions.
	ToolSwitchMap map[string][]string `yaml:"toolSwitchMap,omitempty" json:"toolSwitchMap,omitempty"`
	// RedactKeys masks top-level arg keys in approval prompts (merged with per-call redactKeys).
	RedactKeys []string `yaml:"redactKeys,omitempty" json:"redactKeys,omitempty"`
}

// HitlInterruptConfig is per-tool review configuration at an approval gate.
type HitlInterruptConfig struct {
	AllowedDecisions []HitlDecisionKind  `yaml:"allowedDecisions,omitempty" json:"allowedDecisions,omitempty"`
	Description      string              `yaml:"description,omitempty" json:"description,omitempty"`
	AllowedEditArgs  []string            `yaml:"allowedEditArgs,omitempty" json:"allowedEditArgs,omitempty"`
	DeniedEditArgs   []string            `yaml:"deniedEditArgs,omitempty" json:"deniedEditArgs,omitempty"`
	AllowedEditPaths []string            `yaml:"allowedEditPaths,omitempty" json:"allowedEditPaths,omitempty"`
	DeniedEditPaths  []string            `yaml:"deniedEditPaths,omitempty" json:"deniedEditPaths,omitempty"`
	AllowedEditTools []string            `yaml:"allowedEditTools,omitempty" json:"allowedEditTools,omitempty"`
	SwitchMap        map[string][]string `yaml:"switchMap,omitempty" json:"switchMap,omitempty"`
	RedactKeys       []string            `yaml:"redactKeys,omitempty" json:"redactKeys,omitempty"`
}

// HitlInterruptValue is either enabled-with-defaults (true) or an explicit [HitlInterruptConfig].
type HitlInterruptValue struct {
	Enabled bool
	Config  *HitlInterruptConfig
}

// UnmarshalYAML accepts `true` or a mapping for interruptOn entries.
func (v *HitlInterruptValue) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	switch value.Kind {
	case yaml.ScalarNode:
		var b bool
		if err := value.Decode(&b); err != nil {
			return fmt.Errorf("spec: interruptOn entry must be true or a config object: %w", err)
		}
		if !b {
			return fmt.Errorf("spec: interruptOn entry must be true or a config object, not false")
		}
		v.Enabled = true
		return nil
	case yaml.MappingNode:
		var cfg HitlInterruptConfig
		if err := value.Decode(&cfg); err != nil {
			return fmt.Errorf("spec: interruptOn config: %w", err)
		}
		v.Enabled = true
		v.Config = &cfg
		return nil
	default:
		return fmt.Errorf("spec: interruptOn entry must be true or a config object")
	}
}

// MarshalYAML encodes as true or the config object.
func (v HitlInterruptValue) MarshalYAML() (any, error) {
	if v.Config != nil {
		return v.Config, nil
	}
	if v.Enabled {
		return true, nil
	}
	return nil, nil
}

// ParseHitlDecisionKind normalizes a CLI decision string.
func ParseHitlDecisionKind(s string) (HitlDecisionKind, error) {
	k := HitlDecisionKind(strings.ToLower(strings.TrimSpace(s)))
	switch k {
	case HitlDecisionApprove, HitlDecisionReject, HitlDecisionEdit, HitlDecisionSwitch:
		return k, nil
	default:
		return "", fmt.Errorf("spec: unknown hitl decision %q (want approve, reject, edit, or switch)", s)
	}
}

// IsValidHitlDecisionKind reports whether k is a known decision kind.
func IsValidHitlDecisionKind(k HitlDecisionKind) bool {
	for _, known := range AllHitlDecisionKinds {
		if k == known {
			return true
		}
	}
	return false
}
