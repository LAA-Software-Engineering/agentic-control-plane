package engine

import (
	"context"
	"time"
)

// withSecondsTimeout returns a child context with timeout when seconds > 0; otherwise parent and a no-op cancel.
func withSecondsTimeout(parent context.Context, seconds int) (context.Context, context.CancelFunc) {
	if seconds <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, time.Duration(seconds)*time.Second)
}
