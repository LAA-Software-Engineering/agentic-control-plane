package engine

import (
	"context"
	"errors"
	"time"
)

// ErrTransientGeneration marks a model [models.ModelClient] error as eligible for one MVP retry
// (design doc section 13.4). Wrappers should use fmt.Errorf("...: %w", ErrTransientGeneration).
var ErrTransientGeneration = errors.New("engine: transient model generation failure")

func isTransientGeneration(err error) bool {
	return err != nil && errors.Is(err, ErrTransientGeneration)
}

// withAgentRetry runs fn once, then once more if the first error is a transient generation failure
// and ctx is not done.
func withAgentRetry(ctx context.Context, fn func() error) error {
	err := fn()
	if err == nil || !isTransientGeneration(err) {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(50 * time.Millisecond):
	}
	return fn()
}
