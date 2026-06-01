package policy

import (
	"errors"
	"fmt"
)

// Reason values for [DeniedError] and trace event data (trace.EventPolicyDenied).
const (
	ReasonMaxWallClock     = "max_wall_clock"
	ReasonMaxCost          = "max_cost"
	ReasonStructuredOutput = "structured_output_required"
	ReasonUnknownTool      = "unknown_tool"
	ReasonApprovalRequired = "approval_required"
	ReasonInvalidUses      = "invalid_uses"
	ReasonDenied           = "denied"
)

// DeniedError is returned when a policy check fails (design doc section 12.2 H).
type DeniedError struct {
	Reason  string
	Message string
	Uses    string
	Extra   map[string]any
}

func (e *DeniedError) Error() string {
	if e == nil {
		return "policy: denied"
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("policy denied: %s", e.Reason)
}

// TraceData returns payload suitable for trace.EventPolicyDenied.
func (e *DeniedError) TraceData() map[string]any {
	if e == nil {
		return map[string]any{"reason": "unknown"}
	}
	m := map[string]any{"reason": e.Reason}
	if e.Uses != "" {
		m["uses"] = e.Uses
	}
	for k, v := range e.Extra {
		m[k] = v
	}
	return m
}

// AsDenied unwraps a *DeniedError from err.
func AsDenied(err error) (*DeniedError, bool) {
	var d *DeniedError
	if errors.As(err, &d) {
		return d, true
	}
	return nil, false
}

func denied(reason, msg string, uses string, extra map[string]any) error {
	return &DeniedError{Reason: reason, Message: msg, Uses: uses, Extra: extra}
}
