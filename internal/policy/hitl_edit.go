package policy

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
)

// HitlDecisionInput is an operator resolution submitted at resume or from an interactive prompt.
type HitlDecisionInput struct {
	Kind         spec.HitlDecisionKind
	Actor        string
	EditedWith   map[string]any
	SwitchTarget string
}

// ApplyHitlDecision resolves the effective uses/with for a gated call after operator input.
func ApplyHitlDecision(gate HitlGate, in HitlDecisionInput) (uses string, with map[string]any, err error) {
	if !decisionAllowed(in.Kind, gate.Review.AllowedDecisions) {
		return "", nil, fmt.Errorf("policy: decision %q is not allowed (allowed: %v)", in.Kind, gate.Review.AllowedDecisions)
	}
	switch in.Kind {
	case spec.HitlDecisionApprove:
		return gate.Uses, shallowCopyMap(gate.With), nil
	case spec.HitlDecisionReject:
		return "", nil, ErrHitlRejected
	case spec.HitlDecisionEdit:
		if in.EditedWith == nil {
			return "", nil, fmt.Errorf("policy: edit decision requires edited arguments")
		}
		if err := ValidateHitlEdit(gate.With, in.EditedWith, gate.Review); err != nil {
			return "", nil, err
		}
		return gate.Uses, shallowCopyMap(in.EditedWith), nil
	case spec.HitlDecisionSwitch:
		target := strings.TrimSpace(in.SwitchTarget)
		if target == "" {
			return "", nil, fmt.Errorf("policy: switch decision requires a target operation")
		}
		if !switchTargetAllowed(target, gate.Review.SwitchTargets) {
			return "", nil, fmt.Errorf("policy: switch target %q is not allowed (allowed: %v)", target, gate.Review.SwitchTargets)
		}
		newUses, err := switchUses(gate.Uses, target)
		if err != nil {
			return "", nil, err
		}
		return newUses, shallowCopyMap(gate.With), nil
	default:
		return "", nil, fmt.Errorf("policy: unknown hitl decision %q", in.Kind)
	}
}

// ErrHitlRejected indicates the operator rejected a gated tool call.
var ErrHitlRejected = fmt.Errorf("policy: hitl decision rejected")

// HitlRejectedError wraps rejection with actor attribution for trace events.
type HitlRejectedError struct {
	Actor string
	Uses  string
}

func (e *HitlRejectedError) Error() string {
	if e == nil {
		return ErrHitlRejected.Error()
	}
	return fmt.Sprintf("policy: operator %q rejected tool call %q", e.Actor, e.Uses)
}

func (e *HitlRejectedError) Unwrap() error { return ErrHitlRejected }

// AsHitlRejected unwraps a [HitlRejectedError].
func AsHitlRejected(err error) (*HitlRejectedError, bool) {
	var r *HitlRejectedError
	if err == nil {
		return nil, false
	}
	if errorsAsHitlRejected(err, &r) {
		return r, true
	}
	return nil, false
}

func errorsAsHitlRejected(err error, target **HitlRejectedError) bool {
	for err != nil {
		if r, ok := err.(*HitlRejectedError); ok {
			*target = r
			return true
		}
		err = unwrapOnce(err)
	}
	return false
}

func unwrapOnce(err error) error {
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return u.Unwrap()
	}
	return nil
}

func decisionAllowed(kind spec.HitlDecisionKind, allowed []spec.HitlDecisionKind) bool {
	for _, a := range allowed {
		if a == kind {
			return true
		}
	}
	return false
}

func switchTargetAllowed(target string, allowed []string) bool {
	target = strings.TrimSpace(target)
	for _, a := range allowed {
		if strings.TrimSpace(a) == target {
			return true
		}
	}
	return false
}

func switchUses(sourceUses, targetOperation string) (string, error) {
	toolName, _, err := tools.ParseUses(sourceUses)
	if err != nil {
		return "", err
	}
	targetOperation = strings.TrimSpace(targetOperation)
	if targetOperation == "" {
		return "", fmt.Errorf("policy: empty switch target operation")
	}
	return fmt.Sprintf("tool.%s.%s", toolName, targetOperation), nil
}

// ValidateHitlEdit ensures edited args respect allow/deny path rules (deny wins).
func ValidateHitlEdit(original, edited map[string]any, review ResolvedHitlReview) error {
	if original == nil {
		original = map[string]any{}
	}
	if edited == nil {
		return fmt.Errorf("policy: edited args must be a non-nil object")
	}
	origFlat := flattenArgs(original)
	editFlat := flattenArgs(edited)
	for path, origVal := range origFlat {
		newVal, ok := editFlat[path]
		if !ok {
			if pathAllowed(path, review.AllowedEditPaths, review.AllowedEditArgs) {
				continue
			}
			return fmt.Errorf("policy: missing required path %q in edited args", path)
		}
		if pathDenied(path, review.DeniedEditPaths, review.DeniedEditArgs) && !valuesEqual(origVal, newVal) {
			return fmt.Errorf("policy: cannot edit denied path %q", path)
		}
		if !pathAllowed(path, review.AllowedEditPaths, review.AllowedEditArgs) && !valuesEqual(origVal, newVal) {
			return fmt.Errorf("policy: cannot change path %q without an allow rule", path)
		}
	}
	for path := range editFlat {
		if _, ok := origFlat[path]; ok {
			continue
		}
		if pathDenied(path, review.DeniedEditPaths, review.DeniedEditArgs) {
			return fmt.Errorf("policy: cannot edit denied path %q", path)
		}
		if !pathAllowed(path, review.AllowedEditPaths, review.AllowedEditArgs) {
			return fmt.Errorf("policy: cannot add path %q without an allow rule", path)
		}
	}
	return nil
}

// HitlArgsDiff returns changed paths between original and edited args for audit traces.
func HitlArgsDiff(original, edited map[string]any) map[string]any {
	origFlat := flattenArgs(original)
	editFlat := flattenArgs(edited)
	diff := map[string]any{}
	for path, newVal := range editFlat {
		oldVal, ok := origFlat[path]
		if !ok {
			diff[path] = map[string]any{"from": nil, "to": newVal}
			continue
		}
		if !valuesEqual(oldVal, newVal) {
			diff[path] = map[string]any{"from": oldVal, "to": newVal}
		}
	}
	for path, oldVal := range origFlat {
		if _, ok := editFlat[path]; !ok {
			diff[path] = map[string]any{"from": oldVal, "to": nil}
		}
	}
	return diff
}

func pathAllowed(path string, allowedPaths, allowedArgs []string) bool {
	if len(allowedPaths) == 0 && len(allowedArgs) == 0 {
		return false
	}
	for _, a := range allowedArgs {
		a = strings.TrimSpace(a)
		if a == "*" || a == path || topLevelKey(path) == a {
			return true
		}
	}
	for _, p := range allowedPaths {
		p = strings.TrimSpace(p)
		if p == "*" || p == path || strings.HasPrefix(path, p+".") {
			return true
		}
	}
	return false
}

func pathDenied(path string, deniedPaths, deniedArgs []string) bool {
	for _, d := range deniedArgs {
		d = strings.TrimSpace(d)
		if d == "*" || d == path || topLevelKey(path) == d {
			return true
		}
	}
	for _, d := range deniedPaths {
		d = strings.TrimSpace(d)
		if d == "*" || d == path || strings.HasPrefix(path, d+".") {
			return true
		}
	}
	return false
}

func topLevelKey(path string) string {
	if i := strings.IndexByte(path, '.'); i >= 0 {
		return path[:i]
	}
	return path
}

func flattenArgs(m map[string]any) map[string]any {
	out := map[string]any{}
	flattenInto("", m, out)
	return out
}

func flattenInto(prefix string, v any, out map[string]any) {
	switch val := v.(type) {
	case map[string]any:
		for k, child := range val {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			if childMap, ok := child.(map[string]any); ok {
				flattenInto(key, childMap, out)
			} else {
				out[key] = child
			}
		}
	default:
		if prefix != "" {
			out[prefix] = v
		}
	}
}

func valuesEqual(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ab) == string(bb)
}

func shallowCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// RedactHitlArgs returns a copy of args with sensitive keys masked for prompts.
func RedactHitlArgs(args map[string]any, redactKeys []string) map[string]any {
	if len(redactKeys) == 0 {
		return shallowCopyMap(args)
	}
	flat := flattenArgs(args)
	redacted := shallowCopyMap(args)
	for path := range flat {
		if pathDenied(path, nil, redactKeys) {
			setNestedValue(redacted, path, "••••••")
		}
	}
	return redacted
}

func setNestedValue(root map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	cur := root
	for i, p := range parts {
		if i == len(parts)-1 {
			cur[p] = value
			return
		}
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
}
