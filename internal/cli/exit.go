package cli

import (
	"errors"
	"fmt"
)

// Exit codes (design doc section 11.2).
const (
	ExitSuccess           = 0
	ExitGenericFailure    = 1
	ExitValidationError   = 2
	ExitPlanApplyConflict = 3
	ExitExecutionError    = 4
	ExitPolicyDenied      = 5
)

// ExitError carries a non-zero exit status for [ExitCodeOf].
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e == nil || e.Err == nil {
		return "exit error"
	}
	return e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *ExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ExitCodeOf maps errors to process exit codes. Unknown errors use [ExitGenericFailure].
func ExitCodeOf(err error) int {
	if err == nil {
		return ExitSuccess
	}
	var ee *ExitError
	if errors.As(err, &ee) && ee != nil && ee.Code >= 0 && ee.Code <= 255 {
		return ee.Code
	}
	return ExitGenericFailure
}

// NewExitError wraps err with a specific exit code.
func NewExitError(code int, err error) error {
	if err == nil {
		return nil
	}
	return &ExitError{Code: code, Err: err}
}

// NewExitErrorf is like [fmt.Errorf] with an exit code.
func NewExitErrorf(code int, format string, a ...any) error {
	return &ExitError{Code: code, Err: fmt.Errorf(format, a...)}
}
