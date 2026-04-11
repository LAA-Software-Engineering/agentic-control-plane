package runtime

import (
	"context"
	"errors"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// Runner drives workflow execution against persistent run and trace state (design doc §5.2).
type Runner struct {
	Runs state.RuntimeStore
}

// NewRunner returns a runner backed by runs.
func NewRunner(runs state.RuntimeStore) *Runner {
	return &Runner{Runs: runs}
}

// PersistRunStart inserts a new run row before execution proceeds.
func (r *Runner) PersistRunStart(ctx context.Context, run state.Run) error {
	if r == nil || r.Runs == nil {
		return errors.New("runtime: nil runtime store")
	}
	return r.Runs.StartRun(ctx, run)
}
