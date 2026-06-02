package spec

import (
	"fmt"
	"sort"
	"strings"
)

// Built-in policy preset names (issue #104).
const (
	PresetStrict     = "strict"
	PresetPermissive = "permissive"
	PresetShellSafe  = "shell_safe"
)

// shellCommandOperations are native tool operations that carry a shell command in step input.
var shellCommandOperations = []string{"command.run", "run", "exec", "shell"}

var shellGateTokens = map[string]struct{}{
	"rm": {}, "mv": {}, "cp": {}, "chmod": {}, "chown": {}, "mkfifo": {}, "dd": {},
	"curl": {}, "wget": {}, "ssh": {}, "exec": {}, "eval": {}, "write": {}, "delete": {},
}

// ErrUnknownPreset is returned when a policy references an unrecognized preset name.
type ErrUnknownPreset struct {
	Name string
}

func (e *ErrUnknownPreset) Error() string {
	if e == nil || e.Name == "" {
		return "policy: unknown preset"
	}
	return fmt.Sprintf("policy: unknown preset %q (valid: %s)", e.Name, strings.Join(BuiltinPresetNames(), ", "))
}

// BuiltinPresetNames returns sorted built-in preset identifiers.
func BuiltinPresetNames() []string {
	names := make([]string, 0, len(builtinPresetBuilders))
	for name := range builtinPresetBuilders {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsBuiltinPreset reports whether name is a built-in preset identifier.
func IsBuiltinPreset(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	_, ok := builtinPresetBuilders[name]
	return ok
}

var builtinPresetBuilders = map[string]func() PolicySpec{
	PresetStrict:     buildStrictPreset,
	PresetPermissive: buildPermissivePreset,
	PresetShellSafe:  buildShellSafePreset,
}

func buildStrictPreset() PolicySpec {
	return PolicySpec{
		ResolvedPreset: PresetStrict,
		Approvals: &PolicyApprovals{
			RequireAllTools: true,
		},
	}
}

func buildPermissivePreset() PolicySpec {
	return PolicySpec{
		ResolvedPreset: PresetPermissive,
		Approvals: &PolicyApprovals{
			Permissive: true,
		},
	}
}

func buildShellSafePreset() PolicySpec {
	return PolicySpec{
		ResolvedPreset: PresetShellSafe,
		Approvals: &PolicyApprovals{
			RequiredFor: shellSafeExpandedRequiredFor(),
		},
	}
}

func shellSafeExpandedRequiredFor() []string {
	out := make([]string, 0, len(shellGateTokens)*len(shellCommandOperations))
	for _, op := range shellCommandOperations {
		for token := range shellGateTokens {
			out = append(out, fmt.Sprintf("preset:%s:%s:%s", PresetShellSafe, op, token))
		}
	}
	sort.Strings(out)
	return out
}

// BuildPreset returns a fresh [PolicySpec] for a built-in preset name.
func BuildPreset(name string) (PolicySpec, error) {
	name = strings.TrimSpace(name)
	build, ok := builtinPresetBuilders[name]
	if !ok {
		return PolicySpec{}, &ErrUnknownPreset{Name: name}
	}
	return build(), nil
}

// MergePolicySpec layers local policy fields on top of a preset base (issue #104).
func MergePolicySpec(base, overlay PolicySpec) PolicySpec {
	out := base
	if overlay.Execution != nil {
		out.Execution = clonePolicyExecution(overlay.Execution)
	}
	if overlay.Tools != nil {
		out.Tools = clonePolicyTools(overlay.Tools)
	}
	if overlay.Approvals != nil {
		out.Approvals = mergePolicyApprovals(base.Approvals, overlay.Approvals)
	}
	if overlay.Security != nil {
		out.Security = clonePolicySecurity(overlay.Security)
	}
	if overlay.ResolvedPreset != "" {
		out.ResolvedPreset = overlay.ResolvedPreset
	}
	return out
}

// ResolvePolicySpec expands Preset (when set) and returns the effective merged policy.
func ResolvePolicySpec(pol *PolicySpec) (*PolicySpec, error) {
	if pol == nil {
		return nil, nil
	}
	presetName := strings.TrimSpace(pol.Preset)
	if presetName == "" {
		cp := *pol
		return &cp, nil
	}
	base, err := BuildPreset(presetName)
	if err != nil {
		return nil, err
	}
	merged := MergePolicySpec(base, *pol)
	merged.Preset = presetName
	merged.ResolvedPreset = presetName
	return &merged, nil
}

func mergePolicyApprovals(base, overlay *PolicyApprovals) *PolicyApprovals {
	if overlay == nil {
		return clonePolicyApprovals(base)
	}
	out := &PolicyApprovals{
		RequireAllTools: overlay.RequireAllTools,
		Permissive:      overlay.Permissive,
	}
	if base != nil {
		if !out.RequireAllTools {
			out.RequireAllTools = base.RequireAllTools
		}
		if !out.Permissive {
			out.Permissive = base.Permissive
		}
	}
	out.RequiredFor = mergePresetRequiredFor(
		presetRequiredForSlice(base),
		presetRequiredForSlice(overlay),
	)
	if out.RequiredFor == nil && !out.RequireAllTools && !out.Permissive {
		return nil
	}
	return out
}

func presetRequiredForSlice(a *PolicyApprovals) []string {
	if a == nil {
		return nil
	}
	return append([]string(nil), a.RequiredFor...)
}

func mergePresetRequiredFor(base, overlay []string) []string {
	if len(overlay) == 0 {
		return append([]string(nil), base...)
	}
	if len(base) == 0 {
		return append([]string(nil), overlay...)
	}
	overrideTools := make(map[string]struct{}, len(overlay))
	for _, r := range overlay {
		if tn := toolNameFromPresetRequiredFor(r); tn != "" {
			overrideTools[tn] = struct{}{}
		}
	}
	var kept []string
	for _, r := range base {
		if tn := toolNameFromPresetRequiredFor(r); tn != "" {
			if _, overridden := overrideTools[tn]; overridden {
				continue
			}
		}
		kept = append(kept, r)
	}
	out := append(kept, overlay...)
	sort.Strings(out)
	return dedupePresetStrings(out)
}

func toolNameFromPresetRequiredFor(entry string) string {
	entry = strings.TrimSpace(entry)
	const prefix = "tool."
	if !strings.HasPrefix(entry, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(entry, prefix)
	i := strings.IndexByte(rest, '.')
	if i <= 0 {
		return ""
	}
	return rest[:i]
}

func dedupePresetStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func clonePolicyExecution(in *PolicyExecution) *PolicyExecution {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func clonePolicyTools(in *PolicyTools) *PolicyTools {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func clonePolicySecurity(in *PolicySecurity) *PolicySecurity {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func clonePolicyApprovals(in *PolicyApprovals) *PolicyApprovals {
	if in == nil {
		return nil
	}
	cp := *in
	if in.RequiredFor != nil {
		cp.RequiredFor = append([]string(nil), in.RequiredFor...)
	}
	return &cp
}

// ResolvedPresetName returns the effective preset mode for a policy spec.
func ResolvedPresetName(pol *PolicySpec) string {
	if pol == nil {
		return ""
	}
	if pol.ResolvedPreset != "" {
		return pol.ResolvedPreset
	}
	return strings.TrimSpace(pol.Preset)
}

// ShellCommandOperations returns native operations subject to shell_safe token classification.
func ShellCommandOperations() []string {
	return append([]string(nil), shellCommandOperations...)
}

// IsShellCommandOperation reports whether operation is a shell command carrier for shell_safe.
func IsShellCommandOperation(operation string) bool {
	op := strings.ToLower(strings.TrimSpace(operation))
	for _, candidate := range shellCommandOperations {
		if op == candidate {
			return true
		}
	}
	return false
}
