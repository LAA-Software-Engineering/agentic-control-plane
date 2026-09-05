// Package toolerr is the shared classification for tool-execution errors in the agent loop
// (issue #451). It is a leaf package (stdlib only) so every executor — native, mcp, http — and the
// engine can import it without a cycle (internal/tools already imports internal/tools/native, so the
// marker cannot live in internal/tools itself).
//
// The classifier is FAIL-CLOSED. An UNMARKED tool error is fatal: it aborts the run and is NEVER
// shown to the model. Only an error an adapter has deliberately wrapped with Recoverable is handed
// back to the agent as an observation it can act on. So a transport/config failure, a sandbox-escape
// rejection, a programming error, or a brand-new error type from the next adapter stays fatal until
// someone classifies it — a diagnostic string never silently becomes prompt text.
package toolerr

import (
	"errors"
	"strings"
)

// ErrRecoverable marks a tool error the agent loop may deliver to the model as an observation and
// continue, instead of aborting the run. Match it with errors.Is; construct one with Recoverable.
var ErrRecoverable = errors.New("recoverable tool error")

// RecoverableError is a tool error the agent can correct course over within its iteration budget
// (a file miss, a bad pattern). Observation is a SHORT, MODEL-SAFE description the adapter
// CONSTRUCTS — never the raw underlying Error(), which may embed URLs, request/response bodies, or
// secrets. The underlying err is retained (Unwrap) for the operator log and the redacted audit
// trace; only Observation is ever shown to the model.
type RecoverableError struct {
	Observation string
	Err         error
}

// Recoverable wraps err as a recoverable tool error carrying a model-safe observation. observation
// MUST be adapter-constructed (a classification, the agent's own input echoed back) — do not pass
// err.Error() through it.
func Recoverable(observation string, err error) *RecoverableError {
	return &RecoverableError{Observation: observation, Err: err}
}

func (e *RecoverableError) Error() string {
	switch {
	case e.Err == nil:
		return e.Observation
	case e.Observation == "":
		return e.Err.Error()
	default:
		return e.Observation + ": " + e.Err.Error()
	}
}

// Unwrap exposes the underlying error to the operator-log / redacted-trace path (errors.As, %w),
// NOT to the model — the agent loop reads Observation via SafeObservation.
func (e *RecoverableError) Unwrap() error { return e.Err }

// Is reports RecoverableError as an instance of the ErrRecoverable sentinel.
func (e *RecoverableError) Is(target error) bool { return target == ErrRecoverable }

// SafeObservation returns the model-safe observation text for a tool error and whether err is
// recoverable at all. A recoverable error with no explicit observation falls back to a stable
// generic token — never the raw Error(), so an adapter that forgets a message cannot leak one.
func SafeObservation(err error) (msg string, recoverable bool) {
	var re *RecoverableError
	if errors.As(err, &re) {
		if m := strings.TrimSpace(re.Observation); m != "" {
			return m, true
		}
		return "tool call failed", true
	}
	if errors.Is(err, ErrRecoverable) {
		return "tool call failed", true
	}
	return "", false
}
